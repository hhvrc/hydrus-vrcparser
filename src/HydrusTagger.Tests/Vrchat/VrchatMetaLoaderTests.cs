using HydrusTagger.Taggers.Vrchat;

namespace HydrusTagger.Tests.Vrchat;

/// <summary>
/// Covers <c>db_load_all_parsed_meta</c>'s per-file behaviour: the
/// JSON &gt; XML &gt; line priority contest, and editor provenance which is
/// collected independently of it.
/// </summary>
public class VrchatMetaLoaderTests
{
    private const string VrcxJson = """
        {"application":"VRCX","version":1,
         "author":{"id":"usr_json","displayName":"JsonUser"},
         "world":{"name":"JsonWorld","id":"wrld_json","instanceId":"wrld_json:1"},
         "players":[]}
        """;

    private const string VrcXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
            <rdf:Description>
              <xmp:CreatorTool>VRChat</xmp:CreatorTool>
              <xmp:Author>XmpUser</xmp:Author>
            </rdf:Description>
            <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
              <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
              <vrc:WorldDisplayName>XmpWorld</vrc:WorldDisplayName>
              <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>
        """;

    /// <summary>Adobe-resaved: no vrc: namespace, VRCX JSON inside dc:description.</summary>
    private const string AdobeXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
         <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
          <rdf:Description rdf:about=""
            xmlns:dc="http://purl.org/dc/elements/1.1/"
            xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmp:CreatorTool="Adobe Photoshop Express (Android)">
           <dc:description>
            <rdf:Alt>
             <rdf:li xml:lang="x-default">{"application":"VRCX","version":1,"author":{"id":"usr_embedded","displayName":"EmbeddedUser"},"world":{"name":"EmbeddedWorld","id":"wrld_embedded","instanceId":"wrld_embedded:7"},"players":[]}</rdf:li>
            </rdf:Alt>
           </dc:description>
          </rdf:Description>
         </rdf:RDF>
        </x:xmpmeta>
        """;

    /// <summary>An Adobe packet from an image that was never a VRChat screenshot.</summary>
    private const string NonVrchatXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
         <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
          <rdf:Description rdf:about=""
            xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmp:CreatorTool="GIMP 2.10.34"/>
         </rdf:RDF>
        </x:xmpmeta>
        """;

    private const string LegacyLine =
        "screenshotmanager|0|author:usr_line,LineUser|world:wrld_line,99,LineWorld";

    private static VrcChunk Description(string text, string? contentType) =>
        new(VrcContentType.DescriptionKeyword, text, contentType);

    [Fact]
    public void ReturnsNullWhenNoChunkYieldsMetadata()
    {
        Assert.Null(VrchatMetaLoader.Load([]));
        Assert.Null(VrchatMetaLoader.Load([Description("not metadata at all", VrcContentType.Text)]));
    }

    [Fact]
    public void IgnoresChunksWithUnrelatedKeywords()
    {
        // GIMP comments and GameDVR chunks live in the same table.
        Assert.Null(VrchatMetaLoader.Load([new VrcChunk("Comment", VrcxJson, VrcContentType.Json)]));
    }

    [Fact]
    public void ParsesVrcxJson()
    {
        var meta = VrchatMetaLoader.Load([Description(VrcxJson, VrcContentType.Json)]);

        Assert.NotNull(meta);
        Assert.Equal("usr_json", meta.Author.Id);
        Assert.Equal("JsonWorld", meta.World.Name);
    }

    [Fact]
    public void ParsesXmp()
    {
        var meta = VrchatMetaLoader.Load([Description(VrcXmp, VrcContentType.Xml)]);

        Assert.NotNull(meta);
        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", meta.Author.Id);
        Assert.Equal("XmpWorld", meta.World.Name);
    }

    [Fact]
    public void ParsesTheLegacyLineFormat()
    {
        var meta = VrchatMetaLoader.Load([Description(LegacyLine, VrcContentType.Line)]);

        Assert.NotNull(meta);
        Assert.Equal("usr_line", meta.Author.Id);
        Assert.Equal("LineWorld", meta.World.Name);
    }

    [Fact]
    public void JsonBeatsXmpRegardlessOfChunkOrder()
    {
        var jsonFirst = VrchatMetaLoader.Load(
            [Description(VrcxJson, VrcContentType.Json), Description(VrcXmp, VrcContentType.Xml)]);
        var xmpFirst = VrchatMetaLoader.Load(
            [Description(VrcXmp, VrcContentType.Xml), Description(VrcxJson, VrcContentType.Json)]);

        Assert.Equal("usr_json", jsonFirst!.Author.Id);
        Assert.Equal("usr_json", xmpFirst!.Author.Id);
    }

    [Fact]
    public void XmpBeatsTheLegacyLineFormat()
    {
        var meta = VrchatMetaLoader.Load(
            [Description(LegacyLine, VrcContentType.Line), Description(VrcXmp, VrcContentType.Xml)]);

        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", meta!.Author.Id);
    }

    [Fact]
    public void TheFirstChunkWinsAmongEqualPriority()
    {
        var meta = VrchatMetaLoader.Load(
            [Description(VrcxJson, VrcContentType.Json), Description(VrcxJson.Replace("usr_json", "usr_second", StringComparison.Ordinal), VrcContentType.Json)]);

        Assert.Equal("usr_json", meta!.Author.Id);
    }

    [Fact]
    public void AnXmpChunkThatRecoversEmbeddedJsonIsPromotedToJsonPriority()
    {
        // The recovered payload is a full VRCX record, so it outranks a real
        // XMP packet even though it arrived inside one.
        var meta = VrchatMetaLoader.Load(
            [Description(VrcXmp, VrcContentType.Xml), Description(AdobeXmp, VrcContentType.Xml)]);

        Assert.Equal("usr_embedded", meta!.Author.Id);
        Assert.Equal("EmbeddedWorld", meta.World.Name);
    }

    [Fact]
    public void EditorProvenanceSurvivesLosingThePriorityContest()
    {
        // The point of tracking editor software separately: the JSON chunk wins
        // the metadata contest, but the app that edited the image is only
        // recorded in the XMP chunk.
        var meta = VrchatMetaLoader.Load(
            [Description(VrcxJson, VrcContentType.Json), Description(AdobeXmp, VrcContentType.Xml)]);

        Assert.Equal("usr_json", meta!.Author.Id);
        Assert.Contains("Adobe Photoshop Express (Android)", meta.EditorSoftware);
    }

    [Fact]
    public void EditorProvenanceIsDeduplicatedAcrossChunks()
    {
        var meta = VrchatMetaLoader.Load(
            [Description(AdobeXmp, VrcContentType.Xml), Description(AdobeXmp, VrcContentType.Xml)]);

        Assert.Equal(["Adobe Photoshop Express (Android)"], meta!.EditorSoftware);
    }

    [Fact]
    public void ANonVrchatAdobePacketYieldsNothing()
    {
        // Three quarters of the cached chunks look like this: valid XMP, no
        // vrc: namespace, no embedded VRCX. The line parser then rejects it,
        // and -- matching the Python -- its CreatorTool is lost with it.
        Assert.Null(VrchatMetaLoader.Load([Description(NonVrchatXmp, VrcContentType.Xml)]));
    }

    [Fact]
    public void FallsBackToTheXmlHeuristicWhenTheContentTypeIsMissing()
    {
        var meta = VrchatMetaLoader.Load([Description(VrcXmp, contentType: null)]);

        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", meta!.Author.Id);
    }

    [Fact]
    public void TreatsAnUnrecognizedContentTypeAsTheLineFormat()
    {
        // Migration 004 reclassified these, but a chunk stored as 'text' that
        // is really line format must still parse.
        var meta = VrchatMetaLoader.Load([Description(LegacyLine, VrcContentType.Text)]);

        Assert.Equal("usr_line", meta!.Author.Id);
    }

    [Fact]
    public void ABrokenChunkDoesNotPreventAGoodOneFromWinning()
    {
        var meta = VrchatMetaLoader.Load(
        [
            Description("{ this is not valid json", VrcContentType.Json),
            Description(VrcXmp, VrcContentType.Xml),
        ]);

        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", meta!.Author.Id);
    }

    [Fact]
    public void ReadsTheAdobeXmpKeywordAsWellAsDescription()
    {
        var meta = VrchatMetaLoader.Load(
            [new VrcChunk(VrcContentType.AdobeXmpKeyword, VrcXmp, VrcContentType.Xml)]);

        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", meta!.Author.Id);
    }
}
