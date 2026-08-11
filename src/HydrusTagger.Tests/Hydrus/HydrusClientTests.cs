using System.Net;
using System.Text.Json;
using HydrusTagger.Core.Hydrus;

namespace HydrusTagger.Tests.Hydrus;

public class HydrusClientTests
{
    [Fact]
    public async Task SearchSendsJsonEncodedTagsAndReturnsFileIds()
    {
        var handler = new StubHttpMessageHandler().RespondJson("""{ "file_ids": [1, 2, 3] }""");
        var client = TestHydrusClient.Create(handler);

        var ids = await client.SearchFileIdsAsync(
            ["system:filetype is png", "system:has embedded metadata"]);

        Assert.Equal([1, 2, 3], ids);

        var uri = Assert.Single(handler.Requests).RequestUri!;
        Assert.Equal("/get_files/search_files", uri.AbsolutePath);

        // The tags parameter must be a JSON array, URL-encoded.
        var tags = ParseQueryValue(uri, "tags");
        Assert.Equal(
            ["system:filetype is png", "system:has embedded metadata"],
            JsonSerializer.Deserialize<string[]>(tags)!);
        Assert.Equal("true", ParseQueryValue(uri, "return_file_ids"));
    }

    [Fact]
    public async Task SearchIncludesTagServiceKeyOnlyWhenGiven()
    {
        var handler = new StubHttpMessageHandler()
            .RespondJson("""{ "file_ids": [] }""")
            .RespondJson("""{ "file_ids": [] }""");
        var client = TestHydrusClient.Create(handler);

        await client.SearchFileIdsAsync(["a"]);
        await client.SearchFileIdsAsync(["a"], "svc-key");

        Assert.DoesNotContain("tag_service_key", handler.Requests[0].RequestUri!.Query, StringComparison.Ordinal);
        Assert.Equal("svc-key", ParseQueryValue(handler.Requests[1].RequestUri!, "tag_service_key"));
    }

    [Fact]
    public async Task SearchReturnsEmptyWhenHydrusOmitsFileIds()
    {
        var handler = new StubHttpMessageHandler().RespondJson("{}");
        var client = TestHydrusClient.Create(handler);

        Assert.Empty(await client.SearchFileIdsAsync(["a"]));
    }

    [Fact]
    public async Task FileMetadataDeserializesTheFieldsThePipelineUses()
    {
        var handler = new StubHttpMessageHandler().RespondJson("""
            {
              "metadata": [
                {
                  "file_id": 42,
                  "hash": "aabbcc",
                  "size": 1234,
                  "ext": ".png",
                  "width": 1920,
                  "height": 1080,
                  "has_transparency": true,
                  "has_human_readable_embedded_metadata": true,
                  "known_urls": ["https://x.com/someone/status/1"]
                }
              ]
            }
            """);
        var client = TestHydrusClient.Create(handler);

        var row = Assert.Single(await client.GetFileMetadataAsync([42]));

        Assert.Equal(42, row.FileId);
        Assert.Equal("aabbcc", row.Hash);
        Assert.Equal(1234, row.Size);
        Assert.Equal(1920, row.Width);
        Assert.Equal(1080, row.Height);
        Assert.True(row.HasTransparency);
        Assert.True(row.HasHumanReadableEmbeddedMetadata);
        Assert.Equal("png", row.NormalizedExt);
        Assert.Equal(["https://x.com/someone/status/1"], row.AllUrls);
    }

    [Fact]
    public async Task FileMetadataShortCircuitsOnAnEmptyIdList()
    {
        var handler = new StubHttpMessageHandler();
        var client = TestHydrusClient.Create(handler);

        Assert.Empty(await client.GetFileMetadataAsync([]));
        Assert.Empty(handler.Requests);
    }

    [Fact]
    public async Task AddTagsPostsHashesAndServiceKeysToTags()
    {
        var handler = new StubHttpMessageHandler().RespondJson("{}");
        var client = TestHydrusClient.Create(handler);

        await client.AddTagsAsync(["hash1", "hash2"], "svc-key", ["vrchat", "vrchat-world-name:Home"]);

        var request = Assert.Single(handler.Requests);
        Assert.Equal(HttpMethod.Post, request.Method);
        Assert.Equal("/add_tags/add_tags", request.RequestUri!.AbsolutePath);

        using var body = JsonDocument.Parse(handler.RequestBodies[0]!);
        Assert.Equal(
            ["hash1", "hash2"],
            body.RootElement.GetProperty("hashes").EnumerateArray().Select(e => e.GetString()));
        Assert.Equal(
            ["vrchat", "vrchat-world-name:Home"],
            body.RootElement.GetProperty("service_keys_to_tags").GetProperty("svc-key")
                .EnumerateArray().Select(e => e.GetString()));
    }

    [Fact]
    public async Task AddTagsSkipsTheRequestWhenThereIsNothingToSend()
    {
        var handler = new StubHttpMessageHandler();
        var client = TestHydrusClient.Create(handler);

        await client.AddTagsAsync([], "svc-key", ["vrchat"]);
        await client.AddTagsAsync(["hash1"], "svc-key", []);

        Assert.Empty(handler.Requests);
    }

    [Fact]
    public async Task UnauthorizedSurfacesAsApiExceptionWithAKeyHint()
    {
        var handler = new StubHttpMessageHandler()
            .RespondStatus(HttpStatusCode.Unauthorized, "bad key");
        var client = TestHydrusClient.Create(handler);

        var ex = await Assert.ThrowsAsync<HydrusApiException>(() => client.SearchFileIdsAsync(["a"]));

        Assert.Equal(HttpStatusCode.Unauthorized, ex.StatusCode);
        Assert.Contains("API key", ex.Message, StringComparison.Ordinal);
        Assert.Equal("bad key", ex.ResponseBody);
    }

    [Fact]
    public async Task UnreachableHydrusSurfacesAsConnectionException()
    {
        var handler = new StubHttpMessageHandler().Throw<HttpRequestException>();
        var client = TestHydrusClient.Create(handler);

        var ex = await Assert.ThrowsAsync<HydrusConnectionException>(
            () => client.SearchFileIdsAsync(["a"]));

        Assert.Contains(TestHydrusClient.BaseAddress, ex.Message, StringComparison.Ordinal);
    }

    private static string ParseQueryValue(Uri uri, string key)
    {
        foreach (var pair in uri.Query.TrimStart('?').Split('&', StringSplitOptions.RemoveEmptyEntries))
        {
            var idx = pair.IndexOf('=', StringComparison.Ordinal);
            var name = idx < 0 ? pair : pair[..idx];
            if (name == key)
            {
                return idx < 0 ? "" : Uri.UnescapeDataString(pair[(idx + 1)..]);
            }
        }

        throw new InvalidOperationException($"Query parameter '{key}' not present in {uri.Query}");
    }
}
