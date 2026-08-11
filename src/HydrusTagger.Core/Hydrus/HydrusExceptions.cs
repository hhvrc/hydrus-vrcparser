using System.Net;

namespace HydrusTagger.Core.Hydrus;

/// <summary>
/// Base for every failure originating from the Hydrus client API.
/// </summary>
public class HydrusException : Exception
{
    public HydrusException(string message) : base(message) { }
    public HydrusException(string message, Exception inner) : base(message, inner) { }
}

/// <summary>
/// Configuration is unusable (missing key, malformed address). Distinguished
/// from transport failures because it is never worth retrying.
/// </summary>
public sealed class HydrusConfigurationException : HydrusException
{
    public HydrusConfigurationException(string message) : base(message) { }
}

/// <summary>
/// Hydrus was reachable but rejected the request.
/// </summary>
public sealed class HydrusApiException : HydrusException
{
    public HttpStatusCode StatusCode { get; }
    public string? ResponseBody { get; }

    public HydrusApiException(HttpStatusCode statusCode, string? responseBody, string message)
        : base(message)
    {
        StatusCode = statusCode;
        ResponseBody = responseBody;
    }
}

/// <summary>
/// Could not reach Hydrus at all. The legacy Python exited the process here
/// (<c>hydrus-vrcparser.py</c> caught <c>HydrusConnectionError</c> and returned);
/// the host catches this and reports instead.
/// </summary>
public sealed class HydrusConnectionException : HydrusException
{
    public HydrusConnectionException(string message, Exception inner) : base(message, inner) { }
}

/// <summary>
/// A local tag service could not be resolved unambiguously.
/// </summary>
public sealed class HydrusServiceResolutionException : HydrusException
{
    public IReadOnlyList<string> AvailableServices { get; }

    public HydrusServiceResolutionException(string message, IReadOnlyList<string> available)
        : base(available.Count == 0
            ? message
            : $"{message}{Environment.NewLine}Available:{Environment.NewLine}  - {string.Join($"{Environment.NewLine}  - ", available)}")
    {
        AvailableServices = available;
    }
}
