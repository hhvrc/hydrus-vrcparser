using HydrusTagger.Core.Tagging;

namespace HydrusTagger.Tests.Tagging;

public class CodePointStringComparerTests
{
    private static readonly CodePointStringComparer Comparer = CodePointStringComparer.Instance;

    [Fact]
    public void DisagreesWithOrdinalExactlyWhereItIsSupposedTo()
    {
        // The justification for this whole class in one assertion: UTF-16
        // ordinal puts a surrogate pair before U+E000, code point order does
        // not. Latent today -- no tag in the corpus is astral -- but the two
        // orderings must not be allowed to drift apart silently.
        const string Emoji = "\U0001F600";
        const string PrivateUse = "\uE000";

        Assert.True(string.CompareOrdinal(Emoji, PrivateUse) < 0);
        Assert.True(Comparer.Compare(Emoji, PrivateUse) > 0);
    }

    [Fact]
    public void AgreesWithOrdinalForPlainAscii()
    {
        string[] tags = ["vrchat", "vrchat-author-id:usr_1", "creator_tool:VRChat", "editor:adobe"];

        Assert.Equal(
            tags.OrderBy(t => t, StringComparer.Ordinal),
            tags.OrderBy(t => t, Comparer));
    }

    [Fact]
    public void APrefixSortsBeforeTheLongerString()
    {
        Assert.True(Comparer.Compare("vrchat", "vrchat-author-id:usr_1") < 0);
        Assert.True(Comparer.Compare("vrchat-author-id:usr_1", "vrchat") > 0);
    }

    [Fact]
    public void EqualStringsCompareEqual()
    {
        Assert.Equal(0, Comparer.Compare("\U0001F600abc", "\U0001F600abc"));
        Assert.Equal(0, Comparer.Compare("", ""));
    }

    [Fact]
    public void NullsSortFirstAndConsistently()
    {
        Assert.Equal(0, Comparer.Compare(null, null));
        Assert.True(Comparer.Compare(null, "") < 0);
        Assert.True(Comparer.Compare("", null) > 0);
    }

    [Fact]
    public void AnUnpairedSurrogateIsOrderedByItsOwnCodeUnitRatherThanCrashing()
    {
        // C# permits these in a string and Python permits them via
        // surrogatepass. Neither side should throw, and both order them the
        // same way, which is all the hash needs.
        const string Lone = "\uD800";

        Assert.True(Comparer.Compare("\uD7FF", Lone) < 0);
        Assert.True(Comparer.Compare(Lone, "\uE000") < 0);
    }
}
