using System.Globalization;
using System.Text.RegularExpressions;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>
/// Turns normalized VRChat metadata into Hydrus tags. Port of
/// <c>core/tag_builders.py</c>.
/// </summary>
public static partial class VrchatTagBuilder
{
    /// <summary>
    /// Known image-editor vendors mapped to a canonical brand tag. Matched as a
    /// case-insensitive substring against XMP CreatorTool / softwareAgent.
    /// Order matters: the first match wins, so specific names precede generic
    /// vendors ("paintshop" before "corel" would be wrong, "photoshop" before
    /// "adobe" is not, since both yield "adobe").
    /// </summary>
    private static readonly (string Needle, string Brand)[] EditorBrands =
    [
        ("adobe", "adobe"),
        ("photoshop", "adobe"),
        ("lightroom", "adobe"),
        ("gimp", "gimp"),
        ("affinity", "affinity"),
        ("serif", "affinity"),
        ("corel", "corel"),
        ("paintshop", "corel"),
        ("paint.net", "paint.net"),
        ("pixlr", "pixlr"),
        ("picsart", "picsart"),
        ("snapseed", "snapseed"),
        ("photoroom", "photoroom"),
        ("windows photo", "microsoft"),
        ("microsoft", "microsoft"),
    ];

    /// <summary>
    /// Tags for one file. The order matches the Python builder's, which the
    /// hash does not care about but a diff of the two implementations does.
    /// </summary>
    public static List<string> BuildFileTags(VrcMetadata meta)
    {
        ArgumentNullException.ThrowIfNull(meta);

        List<string> tags = ["vrchat"];

        var authorId = meta.Author.Id.Trim();
        var authorName = meta.Author.DisplayName.Trim();
        if (authorId.Length > 0)
        {
            tags.Add($"vrchat-author-id:{authorId}");
        }

        if (authorName.Length > 0)
        {
            tags.Add($"vrchat-author-name:{authorName}");
        }

        var worldId = meta.World.Id.Trim();
        var worldName = meta.World.Name.Trim();
        var instanceId = meta.World.InstanceId.Trim();
        if (worldId.Length > 0)
        {
            tags.Add($"vrchat-world-id:{worldId}");
        }

        if (worldName.Length > 0)
        {
            tags.Add($"vrchat-world-name:{worldName}");
        }

        if (instanceId.Length > 0)
        {
            tags.Add($"vrchat-world-instanceId:{instanceId}");
        }

        foreach (var player in meta.Players)
        {
            var playerId = player.Id.Trim();
            var playerName = player.DisplayName.Trim();
            if (playerId.Length > 0)
            {
                tags.Add($"vrchat-user-id:{playerId}");
            }

            if (playerName.Length > 0)
            {
                tags.Add($"vrchat-user-name:{playerName}");
            }
        }

        var creatorTool = (meta.CreatorTool ?? "").Trim();
        if (creatorTool.Length > 0)
        {
            tags.Add($"creator_tool:{creatorTool}");
        }

        // The creator tool counts as editor provenance too -- an image whose
        // only XMP is "GIMP 2.10" still deserves editor:gimp.
        var software = creatorTool.Length > 0
            ? new List<string>(meta.EditorSoftware.Count + 1) { creatorTool }
            : new List<string>(meta.EditorSoftware.Count);
        software.AddRange(meta.EditorSoftware);
        tags.AddRange(BuildEditorTags(software));

        if (meta.Created is { } created)
        {
            tags.Add($"vrchat-date:{created.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture)}");
        }

        return tags;
    }

    /// <summary>
    /// Turn XMP creator/editor software strings into <c>editor:</c> tags,
    /// emitting both the brand and the full normalized app name:
    /// "Adobe Photoshop Express (Android)" gives <c>editor:adobe</c> and
    /// <c>editor:adobe photoshop express</c>.
    /// </summary>
    /// <remarks>
    /// VRChat itself is skipped: it is the source game, not an external editor.
    /// </remarks>
    public static List<string> BuildEditorTags(IEnumerable<string> softwareStrings)
    {
        ArgumentNullException.ThrowIfNull(softwareStrings);

        var tags = new HashSet<string>(StringComparer.Ordinal);

        foreach (var raw in softwareStrings)
        {
            if (string.IsNullOrEmpty(raw))
            {
                continue;
            }

            var low = raw.ToLowerInvariant();
            if (low.Contains("vrchat", StringComparison.Ordinal))
            {
                continue;
            }

            var app = NormalizeAppName(raw);
            if (app.Length > 0)
            {
                tags.Add($"editor:{app}");
            }

            foreach (var (needle, brand) in EditorBrands)
            {
                if (low.Contains(needle, StringComparison.Ordinal))
                {
                    tags.Add($"editor:{brand}");
                    break;
                }
            }
        }

        var sorted = tags.ToList();
        sorted.Sort(Core.Tagging.CodePointStringComparer.Instance);
        return sorted;
    }

    /// <summary>
    /// Parent -> child tag implications for authors, worlds and players, so an
    /// id tag can imply the display name it was seen with.
    /// </summary>
    /// <remarks>
    /// Nothing consumes these yet -- the legacy pipeline cached them in
    /// <c>tag_mappings</c> and never read the table back. Ported because the
    /// correlation tagger wants exactly this id-to-name index.
    /// </remarks>
    public static List<(string Parent, string Child)> BuildTagMappings(IEnumerable<VrcMetadata> metadata)
    {
        ArgumentNullException.ThrowIfNull(metadata);

        var mappings = new Dictionary<string, HashSet<string>>(StringComparer.Ordinal);

        void Add(string parent, string child)
        {
            if (!mappings.TryGetValue(parent, out var children))
            {
                children = new HashSet<string>(StringComparer.Ordinal);
                mappings[parent] = children;
            }

            children.Add(child);
        }

        foreach (var meta in metadata)
        {
            var authorId = meta.Author.Id.Trim();
            var authorName = meta.Author.DisplayName.Trim();
            if (authorId.Length > 0 && authorName.Length > 0)
            {
                Add($"vrchat-user-id:{authorId}", $"vrchat-user-name:{authorName}");
                Add($"vrchat-author-id:{authorId}", $"vrchat-author-name:{authorName}");
            }

            var worldId = meta.World.Id.Trim();
            var worldName = meta.World.Name.Trim();
            if (worldId.Length > 0 && worldName.Length > 0)
            {
                Add($"vrchat-world-id:{worldId}", $"vrchat-world-name:{worldName}");
            }

            foreach (var player in meta.Players)
            {
                var playerId = player.Id.Trim();
                var playerName = player.DisplayName.Trim();
                if (playerId.Length > 0 && playerName.Length > 0)
                {
                    Add($"vrchat-user-id:{playerId}", $"vrchat-user-name:{playerName}");
                }
            }
        }

        return
        [
            .. mappings.SelectMany(kv => kv.Value.Select(child => (kv.Key, child)))
        ];
    }

    /// <summary>
    /// Lowercase an app name and drop trailing platform/version noise:
    /// "Adobe Photoshop Express (Android)" -> "adobe photoshop express",
    /// "GIMP 2.10.34" -> "gimp".
    /// </summary>
    private static string NormalizeAppName(string raw)
    {
        var s = raw.Trim().ToLowerInvariant();
        s = TrailingParenthetical().Replace(s, "");
        s = TrailingVersion().Replace(s, "");
        return CollapseWhitespace().Replace(s, " ").Trim();
    }

    [GeneratedRegex(@"\s*\([^)]*\)\s*$")]
    private static partial Regex TrailingParenthetical();

    [GeneratedRegex(@"\s+v?\d[\d.\-]*$")]
    private static partial Regex TrailingVersion();

    [GeneratedRegex(@"\s+")]
    private static partial Regex CollapseWhitespace();
}
