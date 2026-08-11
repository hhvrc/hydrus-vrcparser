using System.Net;
using System.Text;

namespace HydrusTagger.Tests.Hydrus;

/// <summary>
/// Records outgoing requests and replays canned responses, so client tests
/// exercise real URL construction and JSON serialization without a live Hydrus.
/// </summary>
internal sealed class StubHttpMessageHandler : HttpMessageHandler
{
    private readonly Queue<Func<HttpRequestMessage, HttpResponseMessage>> _responses = new();

    public List<HttpRequestMessage> Requests { get; } = [];
    public List<string?> RequestBodies { get; } = [];

    public StubHttpMessageHandler RespondJson(string json, HttpStatusCode status = HttpStatusCode.OK)
    {
        _responses.Enqueue(_ => new HttpResponseMessage(status)
        {
            Content = new StringContent(json, Encoding.UTF8, "application/json"),
        });
        return this;
    }

    public StubHttpMessageHandler RespondStatus(HttpStatusCode status, string body = "")
    {
        _responses.Enqueue(_ => new HttpResponseMessage(status)
        {
            Content = new StringContent(body, Encoding.UTF8, "text/plain"),
        });
        return this;
    }

    public StubHttpMessageHandler Throw<TException>() where TException : Exception, new()
    {
        _responses.Enqueue(_ => throw new TException());
        return this;
    }

    protected override async Task<HttpResponseMessage> SendAsync(
        HttpRequestMessage request,
        CancellationToken cancellationToken)
    {
        Requests.Add(request);
        RequestBodies.Add(request.Content is null
            ? null
            : await request.Content.ReadAsStringAsync(cancellationToken));

        if (_responses.Count == 0)
        {
            throw new InvalidOperationException(
                $"No canned response left for {request.Method} {request.RequestUri}");
        }

        return _responses.Dequeue()(request);
    }
}
