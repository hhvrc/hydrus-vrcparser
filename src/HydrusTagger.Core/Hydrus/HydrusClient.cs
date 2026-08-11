using System.Net;
using System.Net.Http.Json;
using System.Text.Json;
using Microsoft.Extensions.Logging;

namespace HydrusTagger.Core.Hydrus;

/// <summary>
/// Thin client over the four Hydrus endpoints this application uses.
/// </summary>
public sealed class HydrusClient : IHydrusClient
{
    public const string HttpClientName = "hydrus";
    public const string AccessKeyHeader = "Hydrus-Client-API-Access-Key";

    internal static readonly JsonSerializerOptions Json = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
        PropertyNameCaseInsensitive = true,
        DictionaryKeyPolicy = JsonNamingPolicy.SnakeCaseLower,
    };

    private readonly HttpClient _http;
    private readonly ILogger<HydrusClient> _log;

    public HydrusClient(HttpClient http, ILogger<HydrusClient> log)
    {
        _http = http;
        _log = log;
    }

    public async Task<IReadOnlyList<HydrusService>> GetLocalTagServicesAsync(CancellationToken ct = default)
    {
        using var doc = await GetJsonAsync("get_services", ct).ConfigureAwait(false);
        return CollectLocalTagServices(doc.RootElement);
    }

    /// <summary>
    /// Collect local tag services from a /get_services response.
    /// </summary>
    /// <remarks>
    /// Ported from <c>twitter_username_tagger.py:97 _collect_local_tag_services</c>
    /// rather than <c>hydrus_io.py:18</c>. The same service is reported under
    /// several top-level keys depending on Hydrus version ("local_tags",
    /// "services", "services_v2"), so we scan every list-valued key *and* the
    /// modern "services" object keyed by service key, deduping by key. The
    /// legacy implementation read only "local_tags" and silently missed
    /// services on newer clients.
    /// </remarks>
    internal static List<HydrusService> CollectLocalTagServices(JsonElement root)
    {
        var byKey = new Dictionary<string, HydrusService>(StringComparer.Ordinal);

        void Consider(JsonElement svc, string? fallbackKey)
        {
            if (svc.ValueKind != JsonValueKind.Object)
            {
                return;
            }

            if (!svc.TryGetProperty("type", out var typeEl)
                || !typeEl.TryGetInt32(out var type)
                || type != HydrusServiceType.LocalTags)
            {
                return;
            }

            var key = svc.TryGetProperty("service_key", out var keyEl) && keyEl.ValueKind == JsonValueKind.String
                ? keyEl.GetString()
                : null;
            key ??= fallbackKey;

            if (string.IsNullOrEmpty(key))
            {
                return;
            }

            var name = svc.TryGetProperty("name", out var nameEl) && nameEl.ValueKind == JsonValueKind.String
                ? nameEl.GetString()
                : null;

            byKey[key] = new HydrusService(key, name, type);
        }

        if (root.ValueKind == JsonValueKind.Array)
        {
            foreach (var item in root.EnumerateArray())
            {
                Consider(item, null);
            }

            return [.. byKey.Values];
        }

        if (root.ValueKind != JsonValueKind.Object)
        {
            return [];
        }

        foreach (var prop in root.EnumerateObject())
        {
            if (prop.Value.ValueKind == JsonValueKind.Array)
            {
                foreach (var item in prop.Value.EnumerateArray())
                {
                    Consider(item, null);
                }
            }
            else if (prop.NameEquals("services") && prop.Value.ValueKind == JsonValueKind.Object)
            {
                // Modern shape: an object keyed by service_key, where the value
                // may omit service_key itself.
                foreach (var svc in prop.Value.EnumerateObject())
                {
                    Consider(svc.Value, svc.Name);
                }
            }
        }

        return [.. byKey.Values];
    }

    public async Task<string> ResolveLocalTagServiceKeyAsync(string? serviceName, CancellationToken ct = default)
    {
        var services = await GetLocalTagServicesAsync(ct).ConfigureAwait(false);

        if (services.Count == 0)
        {
            throw new HydrusServiceResolutionException(
                "No local tag service (type 5) found.", []);
        }

        var available = services.Select(s => s.ToString()).ToList();

        HydrusService chosen;
        if (!string.IsNullOrWhiteSpace(serviceName))
        {
            var matches = services.Where(s => string.Equals(s.Name, serviceName, StringComparison.Ordinal)).ToList();
            if (matches.Count == 0)
            {
                throw new HydrusServiceResolutionException(
                    $"No local tag service named '{serviceName}'.", available);
            }

            if (matches.Count > 1)
            {
                throw new HydrusServiceResolutionException(
                    $"Multiple local tag services named '{serviceName}'.", available);
            }

            chosen = matches[0];
        }
        else
        {
            if (services.Count > 1)
            {
                throw new HydrusServiceResolutionException(
                    "Multiple local tag services found; specify which to use.", available);
            }

            chosen = services[0];
        }

        _log.LogInformation("Using local tag service: {Service}", chosen);
        return chosen.ServiceKey;
    }

    public async Task<IReadOnlyList<int>> SearchFileIdsAsync(
        IReadOnlyList<string> tags,
        string? tagServiceKey = null,
        CancellationToken ct = default)
    {
        var query = new List<string>
        {
            "tags=" + Uri.EscapeDataString(JsonSerializer.Serialize(tags)),
            "return_file_ids=true",
        };

        if (!string.IsNullOrEmpty(tagServiceKey))
        {
            query.Add("tag_service_key=" + Uri.EscapeDataString(tagServiceKey));
        }

        var result = await GetAsync<SearchFilesResponse>(
            "get_files/search_files?" + string.Join('&', query), ct).ConfigureAwait(false);

        return result?.FileIds ?? [];
    }

    public async Task<IReadOnlyList<HydrusFileMetadata>> GetFileMetadataAsync(
        IReadOnlyList<int> fileIds,
        CancellationToken ct = default)
    {
        if (fileIds.Count == 0)
        {
            return [];
        }

        var url = "get_files/file_metadata?file_ids="
                  + Uri.EscapeDataString(JsonSerializer.Serialize(fileIds));

        var result = await GetAsync<FileMetadataResponse>(url, ct).ConfigureAwait(false);
        return result?.Metadata ?? [];
    }

    public async Task AddTagsAsync(
        IReadOnlyList<string> hashes,
        string serviceKey,
        IReadOnlyList<string> tags,
        CancellationToken ct = default)
    {
        if (hashes.Count == 0 || tags.Count == 0)
        {
            return;
        }

        var payload = new AddTagsRequest
        {
            Hashes = [.. hashes],
            ServiceKeysToTags = new Dictionary<string, List<string>>(StringComparer.Ordinal)
            {
                [serviceKey] = [.. tags],
            },
        };

        HttpResponseMessage response;
        try
        {
            response = await _http.PostAsJsonAsync("add_tags/add_tags", payload, Json, ct)
                .ConfigureAwait(false);
        }
        catch (HttpRequestException ex)
        {
            throw new HydrusConnectionException($"Could not reach Hydrus at {_http.BaseAddress}.", ex);
        }

        using (response)
        {
            await EnsureSuccessAsync(response, "add_tags/add_tags", ct).ConfigureAwait(false);
        }
    }

    private async Task<T?> GetAsync<T>(string url, CancellationToken ct)
    {
        HttpResponseMessage response;
        try
        {
            response = await _http.GetAsync(url, ct).ConfigureAwait(false);
        }
        catch (HttpRequestException ex)
        {
            throw new HydrusConnectionException($"Could not reach Hydrus at {_http.BaseAddress}.", ex);
        }

        using (response)
        {
            await EnsureSuccessAsync(response, url, ct).ConfigureAwait(false);
            return await response.Content.ReadFromJsonAsync<T>(Json, ct).ConfigureAwait(false);
        }
    }

    private async Task<JsonDocument> GetJsonAsync(string url, CancellationToken ct)
    {
        HttpResponseMessage response;
        try
        {
            response = await _http.GetAsync(url, ct).ConfigureAwait(false);
        }
        catch (HttpRequestException ex)
        {
            throw new HydrusConnectionException($"Could not reach Hydrus at {_http.BaseAddress}.", ex);
        }

        using (response)
        {
            await EnsureSuccessAsync(response, url, ct).ConfigureAwait(false);
            var stream = await response.Content.ReadAsStreamAsync(ct).ConfigureAwait(false);
            await using (stream.ConfigureAwait(false))
            {
                return await JsonDocument.ParseAsync(stream, cancellationToken: ct).ConfigureAwait(false);
            }
        }
    }

    private static async Task EnsureSuccessAsync(HttpResponseMessage response, string url, CancellationToken ct)
    {
        if (response.IsSuccessStatusCode)
        {
            return;
        }

        var body = await SafeReadBodyAsync(response, ct).ConfigureAwait(false);
        var hint = response.StatusCode is HttpStatusCode.Unauthorized or HttpStatusCode.Forbidden
            ? " (check the API key and that its permissions cover tag search/add)"
            : "";

        throw new HydrusApiException(
            response.StatusCode,
            body,
            $"Hydrus returned {(int)response.StatusCode} {response.ReasonPhrase} for {url}{hint}. {body}");
    }

    private static async Task<string?> SafeReadBodyAsync(HttpResponseMessage response, CancellationToken ct)
    {
        try
        {
            return await response.Content.ReadAsStringAsync(ct).ConfigureAwait(false);
        }
        catch (Exception ex) when (ex is HttpRequestException or IOException or ObjectDisposedException)
        {
            return null;
        }
    }
}
