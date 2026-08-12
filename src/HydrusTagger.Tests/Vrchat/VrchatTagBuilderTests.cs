using HydrusTagger.Taggers.Vrchat;

namespace HydrusTagger.Tests.Vrchat;

/// <summary>Port of <c>tests/test_tag_builders.py</c>.</summary>
public class VrchatTagBuilderTests
{
    private static VrcMetadata Meta(
        string authorId = "", string authorName = "",
        string worldId = "", string worldName = "", string instanceId = "",
        string? creatorTool = null,
        IEnumerable<(string Id, string Name)>? players = null,
        IEnumerable<string>? editorSoftware = null,
        DateTimeOffset? created = null) => new()
        {
            Author = new VrcAuthor { Id = authorId, DisplayName = authorName },
            World = new VrcWorld { Id = worldId, Name = worldName, InstanceId = instanceId },
            CreatorTool = creatorTool,
            Players = [.. (players ?? []).Select(p => new VrcPlayer { Id = p.Id, DisplayName = p.Name })],
            EditorSoftware = [.. editorSoftware ?? []],
            Created = created,
        };

    [Fact]
    public void BuildsAuthorAndWorldTags()
    {
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(authorId: "usr_abc", authorName: "TestUser", worldId: "wrld_xyz", worldName: "MyWorld"));

        Assert.Contains("vrchat", tags);
        Assert.Contains("vrchat-author-id:usr_abc", tags);
        Assert.Contains("vrchat-author-name:TestUser", tags);
        Assert.Contains("vrchat-world-id:wrld_xyz", tags);
        Assert.Contains("vrchat-world-name:MyWorld", tags);
    }

    [Fact]
    public void TagsTheCreatorToolVerbatim()
    {
        var tags = VrchatTagBuilder.BuildFileTags(Meta(creatorTool: "VRChat"));

        Assert.Contains("creator_tool:VRChat", tags);
    }

    [Fact]
    public void KeepsVersionNoiseInTheCreatorToolTagButNotTheEditorTag()
    {
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(creatorTool: "Microsoft Windows Photo Viewer 10.0.26100.1882"));

        Assert.Contains("creator_tool:Microsoft Windows Photo Viewer 10.0.26100.1882", tags);
        Assert.Contains("editor:microsoft windows photo viewer", tags);
    }

    [Fact]
    public void OmitsTheCreatorToolTagWhenThereIsNone()
    {
        var tags = VrchatTagBuilder.BuildFileTags(Meta(authorId: "usr_abc"));

        Assert.DoesNotContain(tags, t => t.StartsWith("creator_tool:", StringComparison.Ordinal));
    }

    [Fact]
    public void DerivesEditorTagsFromEditorSoftware()
    {
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(editorSoftware: ["Adobe Photoshop Express (Android)"]));

        Assert.Contains("editor:adobe", tags);
        Assert.Contains("editor:adobe photoshop express", tags);
    }

    [Fact]
    public void NeverTagsVrchatItselfAsAnEditor()
    {
        // VRChat is the source game, not an external editor.
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(creatorTool: "VRChat", editorSoftware: ["VRChat"]));

        Assert.DoesNotContain(tags, t => t.StartsWith("editor:", StringComparison.Ordinal));
    }

    [Fact]
    public void TagsEveryPlayerPresent()
    {
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(players: [("usr_p1", "Player1"), ("usr_p2", "Player2")]));

        Assert.Contains("vrchat-user-id:usr_p1", tags);
        Assert.Contains("vrchat-user-name:Player1", tags);
        Assert.Contains("vrchat-user-id:usr_p2", tags);
    }

    [Fact]
    public void DatesTheTagInTheTimestampsOwnOffsetNotUtc()
    {
        // 06:45 at +02:00 is still the 30th locally; converting to UTC first
        // would be a different day for late-evening screenshots.
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(created: new DateTimeOffset(2025, 8, 30, 6, 45, 33, TimeSpan.FromHours(2))));

        Assert.Contains("vrchat-date:2025-08-30", tags);
    }

    [Fact]
    public void OmitsTheDateTagWhenThereIsNoTimestamp()
    {
        var tags = VrchatTagBuilder.BuildFileTags(Meta(authorId: "usr_abc"));

        Assert.DoesNotContain(tags, t => t.StartsWith("vrchat-date:", StringComparison.Ordinal));
    }

    [Fact]
    public void EmptyMetadataYieldsOnlyTheMarkerTag()
    {
        Assert.Equal(["vrchat"], VrchatTagBuilder.BuildFileTags(Meta()));
    }

    [Fact]
    public void TagsTheInstanceId()
    {
        var tags = VrchatTagBuilder.BuildFileTags(
            Meta(worldId: "wrld_abc", instanceId: "wrld_abc:12345"));

        Assert.Contains("vrchat-world-instanceId:wrld_abc:12345", tags);
    }

    // ---- editor tags ----

    [Fact]
    public void EmitsBothTheBrandAndTheFullAppName()
    {
        Assert.Equal(
            ["editor:adobe", "editor:adobe photoshop express"],
            VrchatTagBuilder.BuildEditorTags(["Adobe Photoshop Express (Android)"]));
    }

    [Fact]
    public void StripsTrailingVersionNumbers()
    {
        Assert.Equal(["editor:gimp"], VrchatTagBuilder.BuildEditorTags(["GIMP 2.10.34"]));
    }

    [Fact]
    public void MapsPhotoshopToTheAdobeBrand()
    {
        var tags = VrchatTagBuilder.BuildEditorTags(["Adobe Photoshop 25.0"]);

        Assert.Contains("editor:adobe", tags);
        Assert.Contains("editor:adobe photoshop", tags);
    }

    [Fact]
    public void SkipsVrchat()
    {
        Assert.Empty(VrchatTagBuilder.BuildEditorTags(["VRChat"]));
    }

    [Fact]
    public void TagsAnUnknownEditorByAppNameAlone()
    {
        Assert.Equal(["editor:somerandomtool"], VrchatTagBuilder.BuildEditorTags(["SomeRandomTool"]));
    }

    [Fact]
    public void DeduplicatesAcrossInputs()
    {
        var tags = VrchatTagBuilder.BuildEditorTags(
            ["Adobe Photoshop Express (Android)", "Adobe Photoshop Express (Android)"]);

        Assert.Equal(["editor:adobe", "editor:adobe photoshop express"], tags);
    }

    [Fact]
    public void IgnoresEmptyAndBlankInputs()
    {
        Assert.Empty(VrchatTagBuilder.BuildEditorTags([]));
        Assert.Empty(VrchatTagBuilder.BuildEditorTags(["", "   "]));
    }

    [Fact]
    public void TheCreatorToolAlsoCountsAsEditorProvenance()
    {
        // An image whose only XMP is "GIMP 2.10" still earns editor:gimp.
        var tags = VrchatTagBuilder.BuildFileTags(Meta(creatorTool: "GIMP 2.10.34"));

        Assert.Contains("creator_tool:GIMP 2.10.34", tags);
        Assert.Contains("editor:gimp", tags);
    }

    // ---- tag mappings ----

    [Fact]
    public void MapsAnAuthorIdToItsDisplayNameUnderBothPrefixes()
    {
        var mappings = VrchatTagBuilder.BuildTagMappings(
            [Meta(authorId: "usr_abc", authorName: "TestUser")]);

        Assert.Contains(("vrchat-user-id:usr_abc", "vrchat-user-name:TestUser"), mappings);
        Assert.Contains(("vrchat-author-id:usr_abc", "vrchat-author-name:TestUser"), mappings);
    }

    [Fact]
    public void MapsAWorldIdToItsName()
    {
        var mappings = VrchatTagBuilder.BuildTagMappings(
            [Meta(worldId: "wrld_xyz", worldName: "MyWorld")]);

        Assert.Contains(("vrchat-world-id:wrld_xyz", "vrchat-world-name:MyWorld"), mappings);
    }

    [Fact]
    public void MapsPlayerIdsToTheirNames()
    {
        var mappings = VrchatTagBuilder.BuildTagMappings([Meta(players: [("usr_p1", "Player1")])]);

        Assert.Contains(("vrchat-user-id:usr_p1", "vrchat-user-name:Player1"), mappings);
    }

    [Fact]
    public void MappingsRequireBothHalvesOfThePair()
    {
        var mappings = VrchatTagBuilder.BuildTagMappings(
            [Meta(authorId: "usr_abc", worldName: "MyWorld")]);

        Assert.Empty(mappings);
    }

    [Fact]
    public void MappingsAreDeduplicatedAcrossFiles()
    {
        var mappings = VrchatTagBuilder.BuildTagMappings(
        [
            Meta(worldId: "wrld_xyz", worldName: "MyWorld"),
            Meta(worldId: "wrld_xyz", worldName: "MyWorld"),
        ]);

        Assert.Single(mappings);
    }
}
