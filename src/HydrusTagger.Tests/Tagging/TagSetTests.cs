using HydrusTagger.Core.Tagging;

namespace HydrusTagger.Tests.Tagging;

/// <summary>
/// The expected hashes are not hand-computed: they come from running
/// <c>hydrus_io.py tags_hash</c> on the same input. The live database holds
/// 2,274 push rows produced by that function, so any divergence here would
/// silently invalidate all of them and force a full re-push.
/// </summary>
/// <remarks>
/// Non-ASCII characters are written as escapes on purpose -- a literal U+E000
/// is invisible in an editor, and this file's whole job is to be checkable.
/// </remarks>
public class TagSetTests
{
    [Fact]
    public void EmptySetHashesLikePythonsEmptyJoin()
    {
        Assert.Equal(
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
            TagSet.Empty.Hash);
        Assert.True(TagSet.Empty.IsEmpty);
    }

    [Fact]
    public void HashMatchesPythonForAnOrdinaryTagSet()
    {
        var tags = new TagSet(["vrchat-world-name:The Great Pug", "vrchat"]);

        Assert.Equal(
            "44875af9ef7def467833a28250d514893a2e3e9253f98493870902f144816759",
            tags.Hash);
    }

    [Fact]
    public void HashIsIndependentOfTheOrderTagsWereProducedIn()
    {
        var a = new TagSet(["b", "a", "c"]);
        var b = new TagSet(["c", "b", "a"]);

        Assert.Equal(a.Hash, b.Hash);
    }

    [Fact]
    public void DuplicateTagsAreCountedTwiceJustAsPythonDoes()
    {
        // The Python builder can emit a tag twice -- two players sharing a
        // display name -- and its sorted join keeps both. No file in the
        // current corpus does, so this pins the behaviour for the day one does.
        var tags = new TagSet(["vrchat", "vrchat-user-name:Bob", "vrchat-user-name:Bob"]);

        Assert.Equal(
            "d49e3e172d899783d27a58d8dc706dc40363e2d1a7e823dbfc0e49e72b2c0374",
            tags.Hash);
        Assert.Equal(3, tags.Tags.Count);
    }

    [Fact]
    public void AstralCharactersSortAfterTheBmpJustAsPythonDoes()
    {
        // U+1F600 is a surrogate pair, so a UTF-16 ordinal sort places it
        // before U+E000 and U+FF21. Python compares code points and puts it
        // last. No tag in the current corpus is astral, so this guards the
        // first emoji display name rather than fixing a present-day break.
        var tags = new TagSet(
        [
            "vrchat-user-name:\U0001F600",
            "vrchat-user-name:\uE000",
            "vrchat-user-name:\uFF21",
            "vrchat-user-name:z",
        ]);

        Assert.Equal(
            [
                "vrchat-user-name:z",
                "vrchat-user-name:\uE000",
                "vrchat-user-name:\uFF21",
                "vrchat-user-name:\U0001F600",
            ],
            tags.SortedTags);

        Assert.Equal(
            "d3b0b2c63f7f470ae19166ee3d5b61139eaccb079febb2c331ca0872e5de11b5",
            tags.Hash);
    }

    [Fact]
    public void MixedScriptsHashLikePython()
    {
        var tags = new TagSet(["\u00E9", "e", "\u4E2D", "\U0001F921", "\uFFFD"]);

        Assert.Equal(
            "adbd96c807f9b3a8f65641ac148e959ff730d9ef200c35bc267e1f7b001917df",
            tags.Hash);
    }

    [Fact]
    public void ProducedOrderIsPreservedForPushing()
    {
        var tags = new TagSet(["z", "a"]);

        Assert.Equal(["z", "a"], tags.Tags);
        Assert.Equal(["a", "z"], tags.SortedTags);
    }

    [Fact]
    public void MutatingTheSourceListDoesNotChangeARecordedHash()
    {
        var source = new List<string> { "a", "b" };
        var tags = new TagSet(source);
        var before = tags.Hash;

        source.Add("c");

        Assert.Equal(before, tags.Hash);
        Assert.Equal(2, tags.Tags.Count);
    }
}
