using System.Buffers;
using System.Text;

namespace HydrusTagger.Core.Tagging;

/// <summary>
/// Orders strings by Unicode scalar value, matching Python's <c>sorted()</c>.
/// </summary>
/// <remarks>
/// <para>
/// This is deliberately <em>not</em> <see cref="StringComparer.Ordinal"/>. That
/// comparer orders by UTF-16 code unit, which places astral characters (emoji,
/// encoded as a surrogate pair starting at U+D800) <em>before</em> BMP
/// characters in U+E000..U+FFFF -- private use, CJK compatibility ideographs,
/// halfwidth forms. Python compares code points, so it puts emoji last.
/// </para>
/// <para>
/// The pushed tag hash is <c>sha256(join("\n", sorted(tags)))</c>, so an
/// ordering disagreement invalidates the stored hash for every affected file
/// and re-pushes it on every run. Measured against the current corpus no tag
/// set contains an astral character, so ordinal would agree today -- this is
/// insurance against the first emoji display name, not a fix for a live bug.
/// Code point order is also exactly UTF-8 byte order, if that is easier to
/// reason about.
/// </para>
/// </remarks>
public sealed class CodePointStringComparer : IComparer<string>, IEqualityComparer<string>
{
    public static readonly CodePointStringComparer Instance = new();

    private CodePointStringComparer()
    {
    }

    public int Compare(string? x, string? y)
    {
        if (ReferenceEquals(x, y))
        {
            return 0;
        }

        if (x is null)
        {
            return -1;
        }

        if (y is null)
        {
            return 1;
        }

        var a = x.AsSpan();
        var b = y.AsSpan();

        while (!a.IsEmpty && !b.IsEmpty)
        {
            var (runeA, consumedA) = NextScalar(a);
            var (runeB, consumedB) = NextScalar(b);

            if (runeA != runeB)
            {
                return runeA < runeB ? -1 : 1;
            }

            a = a[consumedA..];
            b = b[consumedB..];
        }

        // One ran out: the shorter string is a prefix of the longer, so it sorts first.
        if (a.IsEmpty && b.IsEmpty)
        {
            return 0;
        }

        return a.IsEmpty ? -1 : 1;
    }

    public bool Equals(string? x, string? y) => string.Equals(x, y, StringComparison.Ordinal);

    public int GetHashCode(string obj) => StringComparer.Ordinal.GetHashCode(obj);

    /// <summary>
    /// Decode one scalar. An unpaired surrogate -- which C# permits in a string
    /// and Python permits via surrogatepass -- decodes to its own code unit
    /// value rather than U+FFFD, so both sides still agree on the ordering.
    /// </summary>
    private static (uint Scalar, int Consumed) NextScalar(ReadOnlySpan<char> s)
    {
        var status = Rune.DecodeFromUtf16(s, out var rune, out var consumed);
        return status == OperationStatus.Done
            ? ((uint)rune.Value, consumed)
            : (s[0], 1);
    }
}
