using System.Net;
using HydrusTagger.Core.Hydrus;
using Microsoft.Extensions.Logging.Abstractions;

namespace HydrusTagger.Tests.Hydrus;

public class HydrusRetryHandlerTests
{
    /// <summary>No real waiting; just record what backoff would have been used.</summary>
    private sealed class RecordedDelays
    {
        public List<TimeSpan> Delays { get; } = [];
        public Task Delay(TimeSpan d, CancellationToken ct)
        {
            Delays.Add(d);
            return Task.CompletedTask;
        }
    }

    private static (HttpClient Client, StubHttpMessageHandler Stub, RecordedDelays Delays) Build(int maxRetries)
    {
        var stub = new StubHttpMessageHandler();
        var delays = new RecordedDelays();
        var retry = new HydrusRetryHandler(maxRetries, NullLogger.Instance, delays.Delay)
        {
            InnerHandler = stub,
        };
        return (new HttpClient(retry) { BaseAddress = new Uri(TestHydrusClient.BaseAddress) }, stub, delays);
    }

    [Fact]
    public async Task RetriesTransientStatusThenSucceeds()
    {
        var (client, stub, delays) = Build(maxRetries: 3);
        stub.RespondStatus(HttpStatusCode.ServiceUnavailable)
            .RespondStatus(HttpStatusCode.ServiceUnavailable)
            .RespondJson("""{ "file_ids": [] }""");

        var response = await client.GetAsync("get_files/search_files");

        Assert.Equal(HttpStatusCode.OK, response.StatusCode);
        Assert.Equal(3, stub.Requests.Count);
        Assert.Equal([TimeSpan.FromMilliseconds(250), TimeSpan.FromMilliseconds(500)], delays.Delays);
    }

    [Fact]
    public async Task ReturnsTheLastTransientResponseAfterExhaustingRetries()
    {
        var (client, stub, _) = Build(maxRetries: 2);
        stub.RespondStatus(HttpStatusCode.BadGateway)
            .RespondStatus(HttpStatusCode.BadGateway)
            .RespondStatus(HttpStatusCode.BadGateway);

        var response = await client.GetAsync("get_services");

        Assert.Equal(HttpStatusCode.BadGateway, response.StatusCode);
        Assert.Equal(3, stub.Requests.Count);
    }

    [Fact]
    public async Task DoesNotRetryClientErrors()
    {
        // A bad API key will never succeed on retry; failing fast keeps the
        // error message immediate rather than after several backoffs.
        var (client, stub, delays) = Build(maxRetries: 3);
        stub.RespondStatus(HttpStatusCode.Unauthorized);

        var response = await client.GetAsync("get_services");

        Assert.Equal(HttpStatusCode.Unauthorized, response.StatusCode);
        Assert.Single(stub.Requests);
        Assert.Empty(delays.Delays);
    }

    [Fact]
    public async Task RetriesConnectionFailuresAndRethrowsWhenExhausted()
    {
        var (client, stub, delays) = Build(maxRetries: 2);
        stub.Throw<HttpRequestException>().Throw<HttpRequestException>().Throw<HttpRequestException>();

        await Assert.ThrowsAsync<HttpRequestException>(() => client.GetAsync("get_services"));

        Assert.Equal(3, stub.Requests.Count);
        Assert.Equal(2, delays.Delays.Count);
    }
}
