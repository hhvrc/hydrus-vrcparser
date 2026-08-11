using System.Text.Json;
using HydrusTagger.Core.Hydrus;

namespace HydrusTagger.Tests.Hydrus;

/// <summary>
/// Covers the /get_services response shapes. The legacy Python had two
/// divergent implementations of this; these tests pin the behaviour of the
/// better one (twitter_username_tagger.py:97), which the port adopted.
/// </summary>
public class HydrusServiceResolutionTests
{
    private static List<HydrusService> Collect(string json)
    {
        using var doc = JsonDocument.Parse(json);
        return HydrusClient.CollectLocalTagServices(doc.RootElement);
    }

    [Fact]
    public void ReadsModernServicesObjectKeyedByServiceKey()
    {
        // The modern shape omits service_key inside the value; the object key is it.
        var services = Collect("""
            {
              "services": {
                "6c6f63616c2074616773": { "name": "my tags", "type": 5, "type_pretty": "local tag service" },
                "616c6c206b6e6f776e2074616773": { "name": "all known tags", "type": 10 }
              }
            }
            """);

        var svc = Assert.Single(services);
        Assert.Equal("6c6f63616c2074616773", svc.ServiceKey);
        Assert.Equal("my tags", svc.Name);
        Assert.Equal(HydrusServiceType.LocalTags, svc.Type);
    }

    [Fact]
    public void ReadsLegacyLocalTagsList()
    {
        var services = Collect("""
            {
              "local_tags": [ { "name": "my tags", "service_key": "abc123", "type": 5 } ]
            }
            """);

        var svc = Assert.Single(services);
        Assert.Equal("abc123", svc.ServiceKey);
        Assert.Equal("my tags", svc.Name);
    }

    [Fact]
    public void DedupesServiceReportedUnderSeveralTopLevelKeys()
    {
        // This is exactly why hydrus_io.py's version was wrong: real responses
        // repeat the same service under several keys.
        var services = Collect("""
            {
              "local_tags": [ { "name": "my tags", "service_key": "abc123", "type": 5 } ],
              "services_v2": [ { "name": "my tags", "service_key": "abc123", "type": 5 } ],
              "services": { "abc123": { "name": "my tags", "type": 5 } }
            }
            """);

        Assert.Single(services);
    }

    [Fact]
    public void IgnoresNonLocalTagServices()
    {
        var services = Collect("""
            {
              "services": {
                "a": { "name": "all known files", "type": 11 },
                "b": { "name": "my files",        "type": 2  },
                "c": { "name": "downloader tags", "type": 0  }
              }
            }
            """);

        Assert.Empty(services);
    }

    [Fact]
    public void HandlesBareArrayRoot()
    {
        var services = Collect("""
            [ { "name": "my tags", "service_key": "abc123", "type": 5 } ]
            """);

        Assert.Single(services);
    }

    [Fact]
    public void SkipsEntriesWithNoResolvableKey()
    {
        var services = Collect("""
            { "local_tags": [ { "name": "keyless", "type": 5 } ] }
            """);

        Assert.Empty(services);
    }

    [Fact]
    public async Task ResolvesByNameWhenSeveralServicesExist()
    {
        var client = MakeClient("""
            {
              "services": {
                "k1": { "name": "my tags",   "type": 5 },
                "k2": { "name": "downloads", "type": 5 }
              }
            }
            """);

        Assert.Equal("k2", await client.ResolveLocalTagServiceKeyAsync("downloads"));
    }

    [Fact]
    public async Task ResolvesSingleServiceWithoutAName()
    {
        var client = MakeClient("""{ "services": { "k1": { "name": "my tags", "type": 5 } } }""");

        Assert.Equal("k1", await client.ResolveLocalTagServiceKeyAsync(null));
    }

    [Fact]
    public async Task RefusesToGuessBetweenSeveralServices()
    {
        var client = MakeClient("""
            {
              "services": {
                "k1": { "name": "my tags",   "type": 5 },
                "k2": { "name": "downloads", "type": 5 }
              }
            }
            """);

        var ex = await Assert.ThrowsAsync<HydrusServiceResolutionException>(
            () => client.ResolveLocalTagServiceKeyAsync(null));

        Assert.Equal(2, ex.AvailableServices.Count);
        Assert.Contains("my tags", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public async Task ListsAvailableServicesWhenTheNamedOneIsMissing()
    {
        var client = MakeClient("""{ "services": { "k1": { "name": "my tags", "type": 5 } } }""");

        var ex = await Assert.ThrowsAsync<HydrusServiceResolutionException>(
            () => client.ResolveLocalTagServiceKeyAsync("typo tags"));

        Assert.Contains("typo tags", ex.Message, StringComparison.Ordinal);
        Assert.Contains("my tags", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public async Task ThrowsWhenNoLocalTagServiceExists()
    {
        var client = MakeClient("""{ "services": { "k1": { "name": "my files", "type": 2 } } }""");

        await Assert.ThrowsAsync<HydrusServiceResolutionException>(
            () => client.ResolveLocalTagServiceKeyAsync(null));
    }

    /// <summary>
    /// Regression test against a real /get_services response captured from
    /// Hydrus 681 (API v94). That response reports the same service under
    /// "local_tags", "services" (keyed by service key, with no service_key
    /// field inside the value) and "services_v2" simultaneously -- so it
    /// exercises the dedup and the object-key fallback against the genuine
    /// article rather than a hand-written approximation.
    /// </summary>
    [Fact]
    public void MatchesRealHydrusResponse()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "get_services.json");
        using var doc = JsonDocument.Parse(File.ReadAllText(path));

        var services = HydrusClient.CollectLocalTagServices(doc.RootElement);

        // Two local tag services exist on this client; 12 services in total.
        Assert.Equal(2, services.Count);
        Assert.Equal(
            ["my tags", "pixai tags"],
            services.Select(s => s.Name).Order(StringComparer.Ordinal));
        Assert.All(services, s => Assert.Equal(HydrusServiceType.LocalTags, s.Type));
        Assert.All(services, s => Assert.False(string.IsNullOrEmpty(s.ServiceKey)));
    }

    [Fact]
    public async Task RealResponseRequiresAnExplicitServiceName()
    {
        // Because this client has two local tag services, the no-name path must
        // refuse rather than silently pick one.
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "get_services.json");
        var client = MakeClient(File.ReadAllText(path));

        await Assert.ThrowsAsync<HydrusServiceResolutionException>(
            () => client.ResolveLocalTagServiceKeyAsync(null));
    }

    [Fact]
    public async Task ResolvesTheConfiguredServiceFromTheRealResponse()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "get_services.json");
        var client = MakeClient(File.ReadAllText(path));

        // "my tags" is the service the legacy config.json targets.
        Assert.Equal("6c6f63616c2074616773", await client.ResolveLocalTagServiceKeyAsync("my tags"));
    }

    private static HydrusClient MakeClient(string servicesJson)
    {
        var handler = new StubHttpMessageHandler().RespondJson(servicesJson);
        return TestHydrusClient.Create(handler);
    }
}
