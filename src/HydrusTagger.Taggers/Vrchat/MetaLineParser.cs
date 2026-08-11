using System.Globalization;
using Microsoft.Extensions.Logging;
using Microsoft.Extensions.Logging.Abstractions;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>Raised when a metadata line cannot be parsed at all.</summary>
public sealed class MetaParseException : Exception
{
    public MetaParseException(string message) : base(message) { }
}

/// <summary>
/// Parses the legacy pipe-delimited "screenshotmanager" / "lfs" formats.
/// Port of <c>core/meta_line_parser.py</c>.
/// </summary>
/// <remarks>
/// Parsing is deliberately lenient below the top level: a malformed individual
/// field is logged and skipped rather than discarding the whole record, because
/// partial metadata still yields useful tags. Only a missing/unknown type or a
/// non-integer index is fatal.
/// </remarks>
public static class MetaLineParser
{
    private static readonly string[] KnownTypes = ["screenshotmanager", "lfs"];

    public static VrcMetadata Parse(string line, ILogger? log = null)
    {
        log ??= NullLogger.Instance;

        var meta = new VrcMetadata { RawText = line };

        var parts = line.Split('|').Select(p => p.Trim()).ToArray();
        if (parts.Length < 2)
        {
            throw new MetaParseException($"Line must have at least type and index: '{line}'");
        }

        var metaType = parts[0];
        if (!KnownTypes.Contains(metaType, StringComparer.Ordinal))
        {
            throw new MetaParseException($"Unknown meta type: '{metaType}'");
        }

        meta.Type = metaType;

        if (!int.TryParse(parts[1], NumberStyles.Integer, CultureInfo.InvariantCulture, out var index))
        {
            throw new MetaParseException($"Invalid index (must be int): '{parts[1]}'");
        }

        meta.Index = index;

        foreach (var seg in parts[2..])
        {
            // Older screenshotmanager output emits the world segment bare,
            // without the "world:" prefix.
            if (seg.StartsWith("wrld_", StringComparison.Ordinal))
            {
                TryField(log, "world", () => ParseWorld(seg, meta));
                continue;
            }

            var colon = seg.IndexOf(':', StringComparison.Ordinal);
            if (colon < 0)
            {
                continue;
            }

            var key = seg[..colon];
            var val = seg[(colon + 1)..];

            switch (key)
            {
                case "author":
                    TryField(log, key, () => ParseAuthor(val, meta));
                    break;
                case "world":
                    TryField(log, key, () => ParseWorld(val, meta));
                    break;
                case "pos":
                    TryField(log, key, () => ParsePosition(val, meta));
                    break;
                case "rq":
                    TryField(log, key, () => ParseRq(val, meta));
                    break;
                case "players":
                    TryField(log, key, () => ParsePlayers(val, meta, log));
                    break;
                default:
                    // Unknown keys are ignored.
                    break;
            }
        }

        return meta;
    }

    /// <summary>Attempt to parse one field, keeping the rest of the record on failure.</summary>
    private static void TryField(ILogger log, string field, Action parse)
    {
        try
        {
            parse();
        }
        catch (MetaParseException ex)
        {
            log.LogWarning("Failed to parse {Field}: {Message}", field, ex.Message);
        }
    }

    private static void ParseAuthor(string val, VrcMetadata meta)
    {
        var comma = val.IndexOf(',', StringComparison.Ordinal);
        if (comma < 0)
        {
            throw new MetaParseException($"Invalid author format: '{val}'");
        }

        meta.Author = new VrcAuthor
        {
            Id = val[..comma],
            DisplayName = val[(comma + 1)..],
        };
    }

    private static void ParseWorld(string val, VrcMetadata meta)
    {
        // Split into at most 3: the world name may itself contain commas.
        var first = val.IndexOf(',', StringComparison.Ordinal);
        if (first < 0)
        {
            throw new MetaParseException($"Invalid world format: '{val}'");
        }

        var second = val.IndexOf(',', first + 1);
        if (second < 0)
        {
            throw new MetaParseException($"Invalid world format: '{val}'");
        }

        var worldId = val[..first];
        var instance = val[(first + 1)..second];
        var name = val[(second + 1)..];

        meta.World = new VrcWorld
        {
            Id = worldId,
            // Matches the VRCX JSON convention, where instanceId is prefixed
            // with the world id.
            InstanceId = worldId + ":" + instance,
            Name = name,
        };
    }

    private static void ParsePosition(string val, VrcMetadata meta)
    {
        var coords = val.Split(',');
        if (coords.Length != 3)
        {
            throw new MetaParseException($"Invalid pos format, needs 3 floats: '{val}'");
        }

        if (!TryParseDouble(coords[0], out var x)
            || !TryParseDouble(coords[1], out var y)
            || !TryParseDouble(coords[2], out var z))
        {
            throw new MetaParseException($"Non-numeric pos values: '{val}'");
        }

        meta.Position = new VrcPosition { X = x, Y = y, Z = z };
    }

    private static void ParseRq(string val, VrcMetadata meta)
    {
        if (!int.TryParse(val, NumberStyles.Integer, CultureInfo.InvariantCulture, out var rq))
        {
            throw new MetaParseException($"Invalid rq value (must be int): '{val}'");
        }

        meta.Rq = rq;
    }

    private static void ParsePlayers(string val, VrcMetadata meta, ILogger log)
    {
        var players = new List<VrcPlayer>();

        foreach (var entry in val.Split(';'))
        {
            // Split into at most 5: a display name may contain commas.
            var fields = entry.Split(',', 5);
            if (fields.Length != 5)
            {
                log.LogWarning("Skipping malformed player entry: '{Entry}'", entry);
                continue;
            }

            if (!TryParseDouble(fields[1], out var px)
                || !TryParseDouble(fields[2], out var py)
                || !TryParseDouble(fields[3], out var pz))
            {
                log.LogWarning("Skipping player with invalid coords: '{Entry}'", entry);
                continue;
            }

            players.Add(new VrcPlayer
            {
                Id = fields[0],
                DisplayName = fields[4],
                Position = new VrcPosition { X = px, Y = py, Z = pz },
            });
        }

        meta.Players = players;
    }

    /// <summary>
    /// Float parsing matching Python's <c>float()</c>: invariant culture, and
    /// accepting the same specials ("inf", "nan").
    /// </summary>
    private static bool TryParseDouble(string s, out double value) =>
        double.TryParse(
            s,
            NumberStyles.Float | NumberStyles.AllowThousands,
            CultureInfo.InvariantCulture,
            out value);
}
