using HydrusTagger.Core.Hydrus;
using Microsoft.Extensions.Logging.Abstractions;

namespace HydrusTagger.Tests.Hydrus;

internal static class TestHydrusClient
{
    public const string BaseAddress = "http://127.0.0.1:45869/";

    public static HydrusClient Create(HttpMessageHandler handler)
    {
        var http = new HttpClient(handler) { BaseAddress = new Uri(BaseAddress) };
        http.DefaultRequestHeaders.Add(HydrusClient.AccessKeyHeader, "test-key");
        return new HydrusClient(http, NullLogger<HydrusClient>.Instance);
    }
}
