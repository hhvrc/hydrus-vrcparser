namespace HydrusTagger.Core.Hydrus;

public sealed class HydrusClientOptions
{
    public const string SectionName = "Hydrus";

    /// <summary>Client API address, e.g. <c>http://127.0.0.1:45869</c>.</summary>
    public string Address { get; set; } = "http://127.0.0.1:45869";

    /// <summary>
    /// Client API access key. Never persisted by this application -- supply via
    /// environment variable or user-secrets.
    /// </summary>
    public string ApiKey { get; set; } = "";

    /// <summary>Local tag service to read from and push to.</summary>
    public string? ServiceName { get; set; }

    /// <summary>
    /// Files per batched request. Matches the legacy BATCH_SIZE so request
    /// shapes stay comparable during the port.
    /// </summary>
    public int BatchSize { get; set; } = 256;

    public int MaxRetries { get; set; } = 3;

    public TimeSpan Timeout { get; set; } = TimeSpan.FromMinutes(2);

    public void Validate()
    {
        if (string.IsNullOrWhiteSpace(ApiKey))
        {
            throw new HydrusConfigurationException(
                "No Hydrus API key configured. Set Hydrus:ApiKey via environment "
                + "(HYDRUS__APIKEY) or 'dotnet user-secrets set Hydrus:ApiKey <key>'.");
        }

        if (!Uri.TryCreate(Address, UriKind.Absolute, out var uri)
            || (uri.Scheme != Uri.UriSchemeHttp && uri.Scheme != Uri.UriSchemeHttps))
        {
            throw new HydrusConfigurationException($"Invalid Hydrus address: '{Address}'.");
        }

        if (BatchSize <= 0)
        {
            throw new HydrusConfigurationException($"BatchSize must be positive, got {BatchSize}.");
        }
    }
}
