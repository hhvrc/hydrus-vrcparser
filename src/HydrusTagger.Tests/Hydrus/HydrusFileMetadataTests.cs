using System.Text.Json;
using HydrusTagger.Core.Hydrus;

namespace HydrusTagger.Tests.Hydrus;

/// <summary>
/// Tag/URL extraction across the modern and legacy metadata response shapes,
/// ported from <c>correlate_users.py:79</c> and
/// <c>twitter_username_tagger.py:66</c>.
/// </summary>
public class HydrusFileMetadataTests
{
    private static HydrusFileMetadata Parse(string json) =>
        JsonSerializer.Deserialize<HydrusFileMetadata>(json, HydrusClient.Json)!;

    [Fact]
    public void ReadsCurrentTagsFromModernShape()
    {
        var meta = Parse("""
            {
              "tags": {
                "svc1": { "storage_tags": { "0": ["vrchat", "a"], "1": ["pending-tag"] } },
                "svc2": { "storage_tags": { "0": ["b"] } }
              }
            }
            """);

        // Status "1" (pending) must not leak in -- only current tags count.
        Assert.Equal(["vrchat", "a", "b"], meta.CurrentTagsAcrossServices());
    }

    [Fact]
    public void ReadsCurrentTagsFromLegacyShape()
    {
        var meta = Parse("""
            {
              "service_keys_to_statuses_to_tags": {
                "svc1": { "0": ["vrchat"], "2": ["deleted-tag"] }
              }
            }
            """);

        Assert.Equal(["vrchat"], meta.CurrentTagsAcrossServices());
    }

    [Fact]
    public void AggregatesBothShapesWhenHydrusSendsBoth()
    {
        var meta = Parse("""
            {
              "tags": { "svc1": { "storage_tags": { "0": ["from-modern"] } } },
              "service_keys_to_statuses_to_tags": { "svc1": { "0": ["from-legacy"] } }
            }
            """);

        Assert.Equal(["from-modern", "from-legacy"], meta.CurrentTagsAcrossServices());
    }

    [Fact]
    public void HasNoTagsWhenNeitherShapeIsPresent()
    {
        Assert.Empty(Parse("""{ "file_id": 1 }""").CurrentTagsAcrossServices());
    }

    [Fact]
    public void CollectsUrlsFromBothKnownUrlsAndUrls()
    {
        var meta = Parse("""
            {
              "known_urls": ["https://x.com/a/status/1"],
              "urls": ["https://x.com/b/status/2"]
            }
            """);

        Assert.Equal(["https://x.com/a/status/1", "https://x.com/b/status/2"], meta.AllUrls);
    }

    /// <summary>
    /// Regression test against the real /get_files/file_metadata shape from
    /// Hydrus 681 (API v94), with user data replaced by placeholders. Notably
    /// storage_tags carries both "0" (current) and "2" (deleted) buckets, so
    /// filtering to current is load-bearing rather than defensive.
    /// </summary>
    [Fact]
    public void MatchesRealHydrusMetadataResponse()
    {
        var path = Path.Combine(AppContext.BaseDirectory, "Fixtures", "file_metadata.json");
        var response = JsonSerializer.Deserialize<JsonElement>(File.ReadAllText(path));
        var row = JsonSerializer.Deserialize<HydrusFileMetadata>(
            response.GetProperty("metadata")[0].GetRawText(), HydrusClient.Json)!;

        Assert.Equal(42, row.FileId);
        Assert.Equal("png", row.NormalizedExt);
        Assert.Equal(1500, row.Width);
        Assert.True(row.HasHumanReadableEmbeddedMetadata);
        Assert.Single(row.AllUrls);

        // The deleted bucket must not appear.
        var tags = row.CurrentTagsAcrossServices().ToList();
        Assert.Contains("vrchat", tags);
        Assert.DoesNotContain("deleted-example", tags);
    }

    [Theory]
    [InlineData(".PNG", "png")]
    [InlineData("png", "png")]
    [InlineData(".jpeg", "jpeg")]
    [InlineData("", "png")]
    [InlineData(null, "png")]
    public void NormalizesExtensionAndDefaultsToPng(string? ext, string expected)
    {
        var meta = new HydrusFileMetadata { Ext = ext };
        Assert.Equal(expected, meta.NormalizedExt);
    }
}
