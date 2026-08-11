using System.Net;
using Microsoft.Extensions.Logging;

namespace HydrusTagger.Core.Hydrus;

/// <summary>
/// Retries transient failures with exponential backoff. Hydrus is usually a
/// process on the same machine or LAN, so failures are typically "the client is
/// busy importing" rather than genuine outages -- worth a few retries before
/// failing a batch that may represent thousands of files.
/// </summary>
public sealed class HydrusRetryHandler : DelegatingHandler
{
    private static readonly HttpStatusCode[] TransientStatuses =
    [
        HttpStatusCode.RequestTimeout,
        HttpStatusCode.TooManyRequests,
        HttpStatusCode.InternalServerError,
        HttpStatusCode.BadGateway,
        HttpStatusCode.ServiceUnavailable,
        HttpStatusCode.GatewayTimeout,
    ];

    private readonly int _maxRetries;
    private readonly ILogger _log;
    private readonly Func<TimeSpan, CancellationToken, Task> _delay;

    public HydrusRetryHandler(int maxRetries, ILogger log, Func<TimeSpan, CancellationToken, Task>? delay = null)
    {
        _maxRetries = maxRetries;
        _log = log;
        _delay = delay ?? Task.Delay;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        for (var attempt = 0; ; attempt++)
        {
            HttpResponseMessage? response = null;
            Exception? failure = null;

            try
            {
                response = await base.SendAsync(request, cancellationToken).ConfigureAwait(false);
                if (!TransientStatuses.Contains(response.StatusCode))
                {
                    return response;
                }
            }
            catch (HttpRequestException ex)
            {
                failure = ex;
            }
            catch (TaskCanceledException ex) when (!cancellationToken.IsCancellationRequested)
            {
                // Timeout rather than caller-initiated cancellation.
                failure = ex;
            }

            if (attempt >= _maxRetries)
            {
                if (response is not null)
                {
                    return response;
                }

                throw failure!;
            }

            response?.Dispose();

            var backoff = TimeSpan.FromMilliseconds(250 * Math.Pow(2, attempt));
            _log.LogWarning(
                "Hydrus request to {Path} failed ({Reason}); retry {Attempt}/{Max} in {Delay}ms",
                request.RequestUri?.PathAndQuery,
                failure?.GetType().Name ?? response?.StatusCode.ToString(),
                attempt + 1,
                _maxRetries,
                backoff.TotalMilliseconds);

            await _delay(backoff, cancellationToken).ConfigureAwait(false);
        }
    }
}
