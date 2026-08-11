using System.Text.Json;
using HydrusTagger.Taggers.Vrchat;

namespace HydrusTagger.Tests.Vrchat;

/// <summary>Port of <c>tests/test_normalize_meta.py</c>, adapted to the typed model.</summary>
public class MetaNormalizerTests
{
    private static VrcMetadata FromJson(string json) =>
        MetaNormalizer.FromJson(JsonDocument.Parse(json).RootElement, "raw");

    [Fact]
    public void NormalizesATypicalPayload()
    {
        var r = FromJson("""
            {
              "author": { "id": "usr_abc", "displayName": "User" },
              "world":  { "id": "wrld_xyz", "name": "World" }
            }
            """);

        Assert.Equal("usr_abc", r.Author.Id);
        Assert.Equal("User", r.Author.DisplayName);
        Assert.Equal("wrld_xyz", r.World.Id);
        Assert.Equal("World", r.World.Name);
        Assert.Equal("raw", r.RawText);
    }

    [Fact]
    public void DefaultsEveryMissingField()
    {
        var r = FromJson("{}");

        Assert.Equal("", r.Author.Id);
        Assert.Equal("", r.Author.DisplayName);
        Assert.Equal("", r.World.Id);
        Assert.Equal("", r.World.InstanceId);
        Assert.Equal("", r.World.Name);
        Assert.Equal(0.0, r.Position.X);
        Assert.Equal(0.0, r.Position.Y);
        Assert.Equal(0.0, r.Position.Z);
        Assert.Equal(0, r.Rq);
        Assert.Empty(r.Players);
        Assert.Null(r.Created);
        Assert.Empty(r.EditorSoftware);
    }

    [Fact]
    public void FallsBackFromDisplayNameToName()
    {
        var r = FromJson("""{ "author": { "id": "usr_abc", "name": "FallbackName" } }""");
        Assert.Equal("FallbackName", r.Author.DisplayName);
    }

    [Fact]
    public void CoercesNumericStringsInPosition()
    {
        var r = FromJson("""{ "position": { "x": "1.5", "y": 2.0, "z": "3.5" } }""");

        Assert.Equal(1.5, r.Position.X);
        Assert.Equal(2.0, r.Position.Y);
        Assert.Equal(3.5, r.Position.Z);
    }

    [Fact]
    public void KeepsGoodCoordinatesWhenOneIsUnparseable()
    {
        var r = FromJson("""{ "position": { "x": "not_a_number", "y": 1.0, "z": 2.0 } }""");

        Assert.Equal(0.0, r.Position.X);
        Assert.Equal(1.0, r.Position.Y);
        Assert.Equal(2.0, r.Position.Z);
    }

    [Fact]
    public void TreatsANullAuthorAsAbsent()
    {
        var r = FromJson("""{ "author": null }""");

        Assert.Equal("", r.Author.Id);
        Assert.Equal("", r.Author.DisplayName);
    }

    [Fact]
    public void ReadsPlayers()
    {
        var r = FromJson("""
            { "players": [ { "id": "usr_p1", "displayName": "P1" },
                           { "id": "usr_p2", "displayName": "P2" } ] }
            """);

        Assert.Equal(2, r.Players.Count);
        Assert.Equal("usr_p1", r.Players[0].Id);
        Assert.Equal("P2", r.Players[1].DisplayName);
    }

    [Fact]
    public void SkipsNonObjectPlayerEntries()
    {
        // The Python passed the list straight through, so a bare string here
        // later raised an AttributeError it did not catch. Skipping is safer
        // and cannot change the result for well-formed data.
        var r = FromJson("""{ "players": [ "not-an-object", { "id": "usr_p1" } ] }""");

        Assert.Single(r.Players);
        Assert.Equal("usr_p1", r.Players[0].Id);
    }

    [Theory]
    [InlineData("""{ "rq": 4 }""", 4)]
    [InlineData("""{ "rq": "5" }""", 5)]
    [InlineData("""{ "rq": 5.7 }""", 5)]
    [InlineData("""{ "rq": "5.7" }""", 0)]
    [InlineData("""{ "rq": null }""", 0)]
    public void CoercesRenderQualityLikePythonInt(string json, int expected)
    {
        Assert.Equal(expected, FromJson(json).Rq);
    }

    [Fact]
    public void NormalizesTheRealVrcxShapeFromTheDatabase()
    {
        // Structure taken from a content_type='json' row in vrchat.db.
        var r = FromJson("""
            {
              "application": "VRCX",
              "version": 1,
              "author": { "id": "usr_c348aa66-98e5-4f64-96f3-62e7a14187d1", "displayName": "ComfyHeaven" },
              "world": {
                "name": "Sunset Bash",
                "id": "wrld_f96f9d27-fb3b-4f68-b9d5-94b4d431aab2",
                "instanceId": "wrld_f96f9d27-fb3b-4f68-b9d5-94b4d431aab2:81786~group(grp_d1)~region(us)"
              },
              "players": [ { "id": "usr_db6e5c3a-c84f-4a4f-9929-061f3b73ced8", "displayName": "Nitrosaki" } ]
            }
            """);

        Assert.Equal("ComfyHeaven", r.Author.DisplayName);
        Assert.Equal("Sunset Bash", r.World.Name);
        Assert.StartsWith("wrld_f96f9d27", r.World.InstanceId, StringComparison.Ordinal);
        Assert.Single(r.Players);

        // VRCX payloads carry no type/index/position/rq.
        Assert.Null(r.Type);
        Assert.Null(r.Index);
        Assert.Equal(0, r.Rq);
    }

    [Fact]
    public void PreservesMisEncodedDisplayNames()
    {
        // Real rows contain double-encoded UTF-8; the tag must match byte-for-byte.
        var r = FromJson("""{ "players": [ { "id": "usr_x", "displayName": "Nightâˆ—" } ] }""");
        Assert.Equal("Nightâˆ—", r.Players[0].DisplayName);
    }

    [Fact]
    public void MapsXmpOntoTheCommonSchema()
    {
        var xmp = new XmpMetadata
        {
            CreatorTool = "VRChat",
            AuthorId = "usr_abc",
            AuthorDisplayName = "User",
            WorldId = "wrld_xyz",
            WorldName = "World",
            Created = new DateTimeOffset(2025, 1, 1, 0, 0, 0, TimeSpan.Zero),
        };

        var r = MetaNormalizer.FromXmp(xmp, "raw");

        Assert.Equal("xmp", r.Type);
        Assert.Equal("VRChat", r.CreatorTool);
        Assert.Equal("usr_abc", r.Author.Id);
        Assert.Equal("wrld_xyz", r.World.Id);
        // XMP carries no instance id.
        Assert.Equal("", r.World.InstanceId);
        Assert.Equal(new DateTimeOffset(2025, 1, 1, 0, 0, 0, TimeSpan.Zero), r.Created);
    }

    [Fact]
    public void MapsNullXmpFieldsToEmptyStrings()
    {
        var r = MetaNormalizer.FromXmp(new XmpMetadata { WorldId = "wrld_xyz" }, "raw");

        Assert.Equal("", r.Author.Id);
        Assert.Equal("", r.Author.DisplayName);
        Assert.Equal("", r.World.Name);
    }

    [Fact]
    public void HandlesANonObjectJsonRoot()
    {
        // json.loads accepts bare arrays and scalars; normalizing one must not throw.
        var r = MetaNormalizer.FromJson(JsonDocument.Parse("[1,2,3]").RootElement, "raw");

        Assert.Equal("", r.Author.Id);
        Assert.Empty(r.Players);
    }
}

/// <summary>Port of the format-detection half of <c>tests/test_png_itxt.py</c>.</summary>
public class VrcContentTypeTests
{
    [Theory]
    [InlineData("""{"key": "value"}""")]
    [InlineData("[1, 2, 3]")]
    public void DetectsJson(string text)
    {
        Assert.Equal(VrcContentType.Json, VrcContentType.Detect(text));
    }

    [Fact]
    public void TreatsTheAdobeXmpKeywordAsXmlRegardlessOfContent()
    {
        Assert.Equal(
            VrcContentType.Xml,
            VrcContentType.Detect("anything", VrcContentType.AdobeXmpKeyword));
    }

    [Fact]
    public void DetectsXmpShapedXml()
    {
        const string xml =
            """<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></x:xmpmeta>""";

        Assert.Equal(VrcContentType.Xml, VrcContentType.Detect(xml));
    }

    [Fact]
    public void DetectsLegacyLineFormat()
    {
        Assert.Equal(
            VrcContentType.Line,
            VrcContentType.Detect("screenshotmanager|0|author:usr_abc,TestUser"));
    }

    [Fact]
    public void ReturnsNullForUnrecognizedContent()
    {
        Assert.Null(VrcContentType.Detect("random garbage"));
    }

    [Theory]
    [InlineData("""<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></x:xmpmeta>""", true)]
    [InlineData("""<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>""", true)]
    [InlineData("<root><child/></root>", false)]
    [InlineData("not xml", false)]
    [InlineData("", false)]
    [InlineData(null, false)]
    [InlineData("""{"key": "value"}""", false)]
    public void RecognizesXmpEnvelopes(string? text, bool expected)
    {
        Assert.Equal(expected, VrcContentType.IsXmpXml(text));
    }

    [Fact]
    public void RequiresANamespaceOnTheXmpRoot()
    {
        // The original matched against "}xmpmeta", which a namespace-less root
        // can never satisfy.
        Assert.False(VrcContentType.IsXmpXml("<xmpmeta/>"));
    }
}
