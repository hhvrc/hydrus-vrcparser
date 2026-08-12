using System.Security.Cryptography;
using System.Text;

namespace HydrusTagger.Core.Tagging;

/// <summary>
/// The tags one tagger derived for one file, plus the hash used for change
/// detection.
/// </summary>
/// <remarks>
/// <para>
/// The hash is <c>sha256(join("\n", sorted(tags)))</c> over UTF-8, reproducing
/// <c>hydrus_io.py:112 tags_hash</c> exactly. The existing database holds 2,274
/// push rows computed that way; a divergence here does not corrupt anything but
/// does force a full needless re-push.
/// </para>
/// <para>
/// Duplicates are preserved rather than collapsed, because the Python builder
/// can emit the same tag twice (a world with two players sharing a display
/// name, say) and its hash counts both. No file in the current corpus actually
/// has one, so this only matters for the day one appears. Hydrus does not care
/// either way.
/// </para>
/// </remarks>
public sealed class TagSet
{
    public static readonly TagSet Empty = new([]);

    private readonly string[] _sorted;
    private string? _hash;

    public TagSet(IEnumerable<string> tags)
    {
        ArgumentNullException.ThrowIfNull(tags);

        // Copy rather than alias: a caller reusing its list must not be able to
        // mutate a TagSet whose hash has already been recorded.
        string[] copy = [.. tags];
        Tags = copy;
        _sorted = [.. copy];
        Array.Sort(_sorted, CodePointStringComparer.Instance);
    }

    /// <summary>Tags in the order the tagger produced them.</summary>
    public IReadOnlyList<string> Tags { get; }

    /// <summary>Tags in the order the hash is computed over.</summary>
    public IReadOnlyList<string> SortedTags => _sorted;

    public bool IsEmpty => Tags.Count == 0;

    /// <summary>Lowercase hex SHA-256 of the joined sorted tags.</summary>
    public string Hash => _hash ??= ComputeHash(_sorted);

    public override string ToString() => $"TagSet({Tags.Count} tags, {Hash[..8]})";

    private static string ComputeHash(string[] sorted)
    {
        var joined = string.Join('\n', sorted);
        var digest = SHA256.HashData(Encoding.UTF8.GetBytes(joined));
        return Convert.ToHexStringLower(digest);
    }
}
