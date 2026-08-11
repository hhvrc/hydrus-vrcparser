using HydrusTagger.Taggers.Vrchat;

namespace HydrusTagger.Tests.Vrchat;

/// <summary>Port of <c>tests/test_meta_xmp_parser.py</c>.</summary>
public class XmpMetaParserTests
{
    private const string NormalXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
            <rdf:Description>
              <xmp:CreatorTool>VRChat</xmp:CreatorTool>
              <xmp:Author>TestUser</xmp:Author>
              <xmp:CreateDate>2025-08-30T06:45:33+02:00</xmp:CreateDate>
              <xmp:ModifyDate>2025-08-30T06:45:33+02:00</xmp:ModifyDate>
            </rdf:Description>
            <rdf:Description xmlns:tiff="http://ns.adobe.com/tiff/1.0/">
              <tiff:DateTime>2025-08-30T06:45:33+02:00</tiff:DateTime>
            </rdf:Description>
            <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
              <dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>
            </rdf:Description>
            <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
              <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
              <vrc:WorldDisplayName>Test World</vrc:WorldDisplayName>
              <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>
        """;

    private const string CompactXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
            <rdf:Description>
              <xmp:CreatorTool>VRChat</xmp:CreatorTool>
              <xmp:Author>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</xmp:Author>
            </rdf:Description>
            <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
              <vrc:World>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:World>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>
        """;

    private const string EmptyWorldXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
            <rdf:Description>
              <xmp:CreatorTool>VRChat</xmp:CreatorTool>
              <xmp:Author>SomeUser</xmp:Author>
            </rdf:Description>
            <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
              <vrc:WorldID />
              <vrc:WorldDisplayName></vrc:WorldDisplayName>
              <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>
        """;

    private const string NonVrchatXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="XMP Core 5.5.0">
         <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
          <rdf:Description rdf:about=""
            xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmp:ModifyDate="2025-07-22T17:39:09+02:00">
          </rdf:Description>
         </rdf:RDF>
        </x:xmpmeta>
        """;

    private const string ResavedPhotoViewerXmp = """
        <x:xmpmeta xmlns:x="adobe:ns:meta/">
          <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
            <rdf:Description>
              <xmp:CreatorTool>Microsoft Windows Photo Viewer 10.0.26100.1882</xmp:CreatorTool>
              <xmp:Author>TestUser</xmp:Author>
              <xmp:CreateDate>2025-08-30T06:45:33+02:00</xmp:CreateDate>
            </rdf:Description>
            <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
              <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
              <vrc:WorldDisplayName>Test World</vrc:WorldDisplayName>
              <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
            </rdf:Description>
          </rdf:RDF>
        </x:xmpmeta>
        """;

    /// <summary>
    /// An Adobe-resaved screenshot: the vrc: namespace is gone entirely and the
    /// original VRCX JSON survives only inside dc:description.
    /// </summary>
    private const string EmbeddedVrcxXmp = """
        <?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
        <x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="XMP Core 4.4.0-Exiv2">
         <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
          <rdf:Description rdf:about=""
            xmlns:dc="http://purl.org/dc/elements/1.1/"
            xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmp:CreatorTool="Adobe Photoshop Express (Android)">
           <dc:description>
            <rdf:Alt>
             <rdf:li xml:lang="x-default">{"application":"VRCX","version":1,"author":{"id":"usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd","displayName":"Project"},"world":{"name":"Wild Flower","id":"wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a","instanceId":"wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a:60622~private"},"players":[{"id":"usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd","displayName":"Project"},{"id":"usr_c348aa66-98e5-4f64-96f3-62e7a14187d1","displayName":"ComfyHeaven"}]}</rdf:li>
            </rdf:Alt>
           </dc:description>
          </rdf:Description>
         </rdf:RDF>
        </x:xmpmeta>
        <?xpacket end="w"?>
        """;

    // ---- normal form ----

    [Fact]
    public void ParsesTheNormalForm()
    {
        var r = XmpMetaParser.Parse(NormalXmp);

        Assert.Equal("VRChat", r.CreatorTool);
        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", r.AuthorId);
        Assert.Equal("TestUser", r.AuthorDisplayName);
        Assert.Equal("wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe", r.WorldId);
        Assert.Equal("Test World", r.WorldName);
    }

    [Fact]
    public void PreservesTheOriginalUtcOffsetOnDates()
    {
        var r = XmpMetaParser.Parse(NormalXmp);

        Assert.Equal(new DateTimeOffset(2025, 8, 30, 6, 45, 33, TimeSpan.FromHours(2)), r.Created);

        // DateTimeOffset equality compares instants, so assert the offset too:
        // the date tag is rendered from this value, and normalizing to UTC
        // could move a late-evening screenshot to the previous day.
        Assert.Equal(TimeSpan.FromHours(2), r.Created!.Value.Offset);
        Assert.Equal(30, r.Created.Value.Day);
    }

    [Fact]
    public void NormalizesAZSuffixToUtc()
    {
        var parsed = XmpMetaParser.ParseDateTime("2025-08-30T06:45:33Z");

        Assert.Equal(TimeSpan.Zero, parsed!.Value.Offset);
        Assert.Equal(6, parsed.Value.Hour);
    }

    [Fact]
    public void TreatsAnOffsetLessDateAsUtc()
    {
        var parsed = XmpMetaParser.ParseDateTime("2025-08-30T06:45:33");

        Assert.Equal(TimeSpan.Zero, parsed!.Value.Offset);
        Assert.Equal(6, parsed.Value.Hour);
    }

    [Theory]
    [InlineData(null)]
    [InlineData("")]
    [InlineData("   ")]
    [InlineData("not a date")]
    public void ReturnsNullForUnusableDates(string? value)
    {
        Assert.Null(XmpMetaParser.ParseDateTime(value));
    }

    // ---- compact form ----

    [Fact]
    public void TreatsAuthorAsAnIdOnlyInTheCompactForm()
    {
        var r = XmpMetaParser.Parse(CompactXmp);

        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", r.AuthorId);
        Assert.Null(r.AuthorDisplayName);
        Assert.Equal("wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe", r.WorldId);
    }

    [Fact]
    public void KeepsAuthorAsANameWhenItIsNotAUserId()
    {
        // Compact form, but the author is a display name rather than a usr_ id.
        var xml = CompactXmp.Replace(
            "<xmp:Author>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</xmp:Author>",
            "<xmp:Author>Just A Name</xmp:Author>",
            StringComparison.Ordinal);

        var r = XmpMetaParser.Parse(xml);

        Assert.Null(r.AuthorId);
        Assert.Equal("Just A Name", r.AuthorDisplayName);
    }

    // ---- degenerate but valid ----

    [Fact]
    public void IgnoresAnEmptyWorldIdButKeepsTheAuthor()
    {
        var r = XmpMetaParser.Parse(EmptyWorldXmp);

        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", r.AuthorId);
        Assert.Null(r.WorldId);
        Assert.Equal("SomeUser", r.AuthorDisplayName);
    }

    [Fact]
    public void AcceptsFilesResavedByOtherViewers()
    {
        // Windows Photo Viewer overwrites CreatorTool but preserves vrc: data.
        var r = XmpMetaParser.Parse(ResavedPhotoViewerXmp);

        Assert.Equal("Microsoft Windows Photo Viewer 10.0.26100.1882", r.CreatorTool);
        Assert.Equal("usr_56e86082-c91c-40a4-bb92-2486ceca90eb", r.AuthorId);
        Assert.Equal("TestUser", r.AuthorDisplayName);
        Assert.Equal("Test World", r.WorldName);
    }

    // ---- rejections ----

    [Fact]
    public void RejectsXmpWithNoVrchatIdentifiers()
    {
        Assert.Throws<XmpParseException>(() => XmpMetaParser.Parse(NonVrchatXmp));
    }

    [Theory]
    [InlineData("not xml at all")]
    [InlineData("<root><child/></root>")]
    public void RejectsInvalidOrWronglyRootedXml(string xml)
    {
        Assert.Throws<XmpParseException>(() => XmpMetaParser.Parse(xml));
    }

    [Fact]
    public void RejectsWhenNeitherWorldNorAuthorIdIsPresent()
    {
        const string xml = """
            <x:xmpmeta xmlns:x="adobe:ns:meta/">
              <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
                <rdf:Description>
                  <xmp:CreatorTool>VRChat</xmp:CreatorTool>
                  <xmp:Author>NoIDs</xmp:Author>
                </rdf:Description>
              </rdf:RDF>
            </x:xmpmeta>
            """;

        Assert.Throws<XmpParseException>(() => XmpMetaParser.Parse(xml));
    }

    [Fact]
    public void RejectsDuplicateElementsInTheSameNamespace()
    {
        const string xml = """
            <x:xmpmeta xmlns:x="adobe:ns:meta/">
              <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
                <rdf:Description>
                  <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
                </rdf:Description>
                <rdf:Description>
                  <vrc:WorldID>wrld_00000000-0000-0000-0000-000000000000</vrc:WorldID>
                </rdf:Description>
              </rdf:RDF>
            </x:xmpmeta>
            """;

        var ex = Assert.Throws<XmpParseException>(() => XmpMetaParser.Parse(xml));
        Assert.Contains("Duplicate", ex.Message, StringComparison.Ordinal);
    }

    [Fact]
    public void RejectsMoreThanOneRdfRoot()
    {
        const string xml = """
            <x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
              <rdf:RDF />
              <rdf:RDF />
            </x:xmpmeta>
            """;

        Assert.Throws<XmpParseException>(() => XmpMetaParser.Parse(xml));
    }

    // ---- embedded VRCX JSON recovery ----

    [Fact]
    public void StandardParserRejectsAdobeResavedFiles()
    {
        // No vrc: namespace survives the Adobe round-trip.
        Assert.Throws<XmpParseException>(() => XmpMetaParser.Parse(EmbeddedVrcxXmp));
    }

    [Fact]
    public void RecoversTheVrcxJsonAdobeBuriedInDcDescription()
    {
        var data = XmpMetaParser.ExtractEmbeddedVrcxJson(EmbeddedVrcxXmp);

        Assert.NotNull(data);
        Assert.Equal(
            "wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a",
            data!.Value.GetProperty("world").GetProperty("id").GetString());
        Assert.Equal(
            "usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd",
            data.Value.GetProperty("author").GetProperty("id").GetString());
        Assert.Equal(2, data.Value.GetProperty("players").GetArrayLength());
    }

    [Fact]
    public void FindsNoEmbeddedJsonInPlainOrNativeXmp()
    {
        Assert.Null(XmpMetaParser.ExtractEmbeddedVrcxJson(NonVrchatXmp));

        // Native VRChat data lives in elements, not an embedded JSON blob.
        Assert.Null(XmpMetaParser.ExtractEmbeddedVrcxJson(NormalXmp));
    }

    [Fact]
    public void ReturnsNullRatherThanThrowingOnInvalidXml()
    {
        Assert.Null(XmpMetaParser.ExtractEmbeddedVrcxJson("not xml at all"));
    }

    // ---- editor provenance ----

    [Fact]
    public void ReadsCreatorToolFromAnAttribute()
    {
        // Adobe's compact RDF form puts CreatorTool on the Description element.
        Assert.Contains(
            "Adobe Photoshop Express (Android)",
            XmpMetaParser.ExtractEditorSoftware(EmbeddedVrcxXmp));
    }

    [Fact]
    public void ReadsCreatorToolFromAnElement()
    {
        Assert.Equal(["VRChat"], XmpMetaParser.ExtractEditorSoftware(NormalXmp));
    }

    [Fact]
    public void ReadsSoftwareAgentsFromTheEditHistory()
    {
        const string xml = """
            <x:xmpmeta xmlns:x="adobe:ns:meta/">
              <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
                       xmlns:xmp="http://ns.adobe.com/xap/1.0/"
                       xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
                       xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
                <rdf:Description>
                  <xmp:CreatorTool>GIMP 2.10.34</xmp:CreatorTool>
                  <xmpMM:History>
                    <rdf:Seq>
                      <rdf:li stEvt:action="saved" stEvt:softwareAgent="Adobe Photoshop 25.0"/>
                    </rdf:Seq>
                  </xmpMM:History>
                </rdf:Description>
              </rdf:RDF>
            </x:xmpmeta>
            """;

        var agents = XmpMetaParser.ExtractEditorSoftware(xml);

        Assert.Contains("GIMP 2.10.34", agents);
        Assert.Contains("Adobe Photoshop 25.0", agents);
    }

    [Fact]
    public void DedupesAgentsWhilePreservingOrder()
    {
        const string xml = """
            <x:xmpmeta xmlns:x="adobe:ns:meta/">
              <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
                       xmlns:xmp="http://ns.adobe.com/xap/1.0/"
                       xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
                       xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
                <rdf:Description>
                  <xmp:CreatorTool>Adobe Photoshop Express (Android)</xmp:CreatorTool>
                  <xmpMM:History>
                    <rdf:Seq>
                      <rdf:li stEvt:softwareAgent="Adobe Photoshop Express (Android)"/>
                    </rdf:Seq>
                  </xmpMM:History>
                </rdf:Description>
              </rdf:RDF>
            </x:xmpmeta>
            """;

        Assert.Equal(["Adobe Photoshop Express (Android)"], XmpMetaParser.ExtractEditorSoftware(xml));
    }

    [Fact]
    public void ReturnsNoAgentsForInvalidXml()
    {
        Assert.Empty(XmpMetaParser.ExtractEditorSoftware("not xml"));
    }
}
