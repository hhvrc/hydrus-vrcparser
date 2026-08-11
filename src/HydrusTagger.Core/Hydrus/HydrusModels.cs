using System.Text.Json.Serialization;

namespace HydrusTagger.Core.Hydrus;

/// <summary>
/// Hydrus service type codes. Only the ones we care about.
/// </summary>
public static class HydrusServiceType
{
    public const int LocalTags = 5;
}

/// <summary>Tag status codes inside a service's storage_tags map.</summary>
public static class HydrusTagStatus
{
    /// <summary>Current (as opposed to pending/deleted/petitioned).</summary>
    public const string Current = "0";
}

public sealed record HydrusService(string ServiceKey, string? Name, int Type)
{
    public override string ToString() => $"{Name ?? "?"} (key={ServiceKey})";
}

public sealed class HydrusServiceTags
{
    public string? Name { get; set; }
    public int Type { get; set; }

    /// <summary>Status code ("0" = current) to tag list, as stored.</summary>
    public Dictionary<string, List<string>>? StorageTags { get; set; }

    public Dictionary<string, List<string>>? DisplayTags { get; set; }
}

public sealed class HydrusFileMetadata
{
    public int? FileId { get; set; }
    public string? Hash { get; set; }
    public long? Size { get; set; }
    public string? Ext { get; set; }
    public int? Width { get; set; }
    public int? Height { get; set; }
    public bool HasTransparency { get; set; }
    public bool HasHumanReadableEmbeddedMetadata { get; set; }

    public List<string>? KnownUrls { get; set; }

    /// <summary>Some Hydrus versions report the same data under "urls".</summary>
    [JsonPropertyName("urls")]
    public List<string>? Urls { get; set; }

    /// <summary>Modern shape: service_key -> tag buckets.</summary>
    public Dictionary<string, HydrusServiceTags>? Tags { get; set; }

    /// <summary>Legacy shape: service_key -> status -> tags.</summary>
    public Dictionary<string, Dictionary<string, List<string>>>? ServiceKeysToStatusesToTags { get; set; }

    /// <summary>
    /// Normalized file extension without the leading dot, defaulting to "png"
    /// (matches <c>hydrus_io.py:56</c>).
    /// </summary>
    public string NormalizedExt =>
        string.IsNullOrWhiteSpace(Ext) ? "png" : Ext.TrimStart('.').ToLowerInvariant();

    /// <summary>All URLs recorded against this file, from either response shape.</summary>
    public IEnumerable<string> AllUrls
    {
        get
        {
            if (KnownUrls is not null)
            {
                foreach (var u in KnownUrls)
                {
                    yield return u;
                }
            }

            if (Urls is not null)
            {
                foreach (var u in Urls)
                {
                    yield return u;
                }
            }
        }
    }

    /// <summary>
    /// Current tags aggregated across every tag service, so manually-added tags
    /// count too. Handles both the modern and legacy response shapes -- ported
    /// from <c>correlate_users.py:79 extract_id_name</c>.
    /// </summary>
    public IEnumerable<string> CurrentTagsAcrossServices()
    {
        if (Tags is not null)
        {
            foreach (var svc in Tags.Values)
            {
                if (svc.StorageTags?.TryGetValue(HydrusTagStatus.Current, out var current) == true)
                {
                    foreach (var t in current)
                    {
                        yield return t;
                    }
                }
            }
        }

        if (ServiceKeysToStatusesToTags is not null)
        {
            foreach (var svc in ServiceKeysToStatusesToTags.Values)
            {
                if (svc.TryGetValue(HydrusTagStatus.Current, out var current))
                {
                    foreach (var t in current)
                    {
                        yield return t;
                    }
                }
            }
        }
    }
}

internal sealed class SearchFilesResponse
{
    public List<int>? FileIds { get; set; }
}

internal sealed class FileMetadataResponse
{
    public List<HydrusFileMetadata>? Metadata { get; set; }
}

internal sealed class AddTagsRequest
{
    public required List<string> Hashes { get; set; }
    public required Dictionary<string, List<string>> ServiceKeysToTags { get; set; }
}
