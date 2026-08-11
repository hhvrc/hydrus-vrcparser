using System.Globalization;

namespace HydrusTagger.Tests;

/// <summary>
/// Guards the build configuration the rest of the port depends on. These are
/// cheap, but they fail loudly if someone edits Directory.Build.props in a way
/// that would silently break tag-hash parity with the Python implementation.
/// </summary>
public class ScaffoldTests
{
    [Fact]
    public void InvariantGlobalizationIsEnabled()
    {
        // In globalization-invariant mode the runtime refuses to construct any
        // non-invariant culture at all. That refusal is the guarantee we want:
        // no culture-aware collation can reach a tag sort and reorder it
        // differently from Python's ordinal sorted().
        Assert.Throws<CultureNotFoundException>(() => CultureInfo.GetCultureInfo("tr-TR"));
        Assert.Equal(CultureInfo.InvariantCulture.Name, CultureInfo.CurrentCulture.Name);
    }

    [Fact]
    public void OrdinalSortMatchesPythonSortedOrder()
    {
        // Python's sorted() on str is ordinal by code point. StringComparer.Ordinal
        // must agree, including on the ':' separator used in every tag we emit.
        string[] tags = ["vrchat-world-name:Zed", "vrchat", "vrchat-author-id:usr_1", "VRChat"];
        var sorted = tags.OrderBy(t => t, StringComparer.Ordinal).ToArray();

        Assert.Equal(
            ["VRChat", "vrchat", "vrchat-author-id:usr_1", "vrchat-world-name:Zed"],
            sorted);
    }
}
