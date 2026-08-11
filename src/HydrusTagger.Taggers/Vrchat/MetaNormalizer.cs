using System.Globalization;
using System.Text.Json;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>
/// Normalizes each source format to <see cref="VrcMetadata"/>. Port of
/// <c>db_logic.py:_normalize_meta</c>, moved out of the data layer into the
/// tagger that owns the schema.
/// </summary>
/// <remarks>
/// Field-level tolerance is the point: a record with an unparseable position
/// still yields its author and world tags. Missing strings normalize to empty
/// rather than null so tag building can test them uniformly.
/// </remarks>
public static class MetaNormalizer
{
    /// <summary>Normalize a VRCX JSON payload.</summary>
    public static VrcMetadata FromJson(JsonElement root, string rawText)
    {
        var meta = new VrcMetadata { RawText = rawText };

        if (root.ValueKind != JsonValueKind.Object)
        {
            return meta;
        }

        meta.Type = StringOrNull(root, "type");
        meta.Index = IntOrNull(root, "index");
        meta.CreatorTool = StringOrNull(root, "creator_tool");

        if (TryObject(root, "author", out var author))
        {
            meta.Author = new VrcAuthor
            {
                Id = StringOrEmpty(author, "id"),
                // VRCX writes displayName; some older payloads use name.
                DisplayName = FirstNonEmpty(StringOrEmpty(author, "displayName"), StringOrEmpty(author, "name")),
            };
        }

        if (TryObject(root, "world", out var world))
        {
            meta.World = new VrcWorld
            {
                Id = StringOrEmpty(world, "id"),
                InstanceId = StringOrEmpty(world, "instanceId"),
                Name = StringOrEmpty(world, "name"),
            };
        }

        if (TryObject(root, "position", out var position))
        {
            meta.Position = ReadPosition(position);
        }

        meta.Rq = ReadRq(root);

        if (root.TryGetProperty("players", out var players) && players.ValueKind == JsonValueKind.Array)
        {
            foreach (var entry in players.EnumerateArray())
            {
                // The Python passed the raw list straight through, so a
                // non-object entry would later crash tag building with an
                // AttributeError it did not catch. Skipping is strictly safer
                // and cannot change the result for well-formed data.
                if (entry.ValueKind != JsonValueKind.Object)
                {
                    continue;
                }

                meta.Players.Add(new VrcPlayer
                {
                    Id = StringOrEmpty(entry, "id"),
                    DisplayName = FirstNonEmpty(
                        StringOrEmpty(entry, "displayName"), StringOrEmpty(entry, "name")),
                    Position = TryObject(entry, "position", out var pp) ? ReadPosition(pp) : new VrcPosition(),
                });
            }
        }

        return meta;
    }

    /// <summary>Normalize a native VRChat XMP packet.</summary>
    public static VrcMetadata FromXmp(XmpMetadata xmp, string rawText) => new()
    {
        RawText = rawText,
        Type = "xmp",
        CreatorTool = xmp.CreatorTool,
        Author = new VrcAuthor
        {
            Id = xmp.AuthorId ?? "",
            DisplayName = xmp.AuthorDisplayName ?? "",
        },
        World = new VrcWorld
        {
            Id = xmp.WorldId ?? "",
            // XMP carries no instance id.
            InstanceId = "",
            Name = xmp.WorldName ?? "",
        },
        Created = xmp.Created,
    };

    private static VrcPosition ReadPosition(JsonElement obj) => new()
    {
        X = DoubleOrZero(obj, "x"),
        Y = DoubleOrZero(obj, "y"),
        Z = DoubleOrZero(obj, "z"),
    };

    private static int ReadRq(JsonElement root)
    {
        if (!root.TryGetProperty("rq", out var rq))
        {
            return 0;
        }

        return rq.ValueKind switch
        {
            // Python int() truncates toward zero.
            JsonValueKind.Number when rq.TryGetDouble(out var d) => (int)Math.Truncate(d),
            // Python int("5") works; int("5.7") raises and leaves the default.
            JsonValueKind.String when int.TryParse(
                rq.GetString(), NumberStyles.Integer, CultureInfo.InvariantCulture, out var i) => i,
            _ => 0,
        };
    }

    private static bool TryObject(JsonElement parent, string name, out JsonElement value)
    {
        if (parent.TryGetProperty(name, out value) && value.ValueKind == JsonValueKind.Object)
        {
            return true;
        }

        value = default;
        return false;
    }

    private static string StringOrEmpty(JsonElement parent, string name) =>
        parent.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String
            ? v.GetString() ?? ""
            : "";

    private static string? StringOrNull(JsonElement parent, string name) =>
        parent.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.String
            ? v.GetString()
            : null;

    private static int? IntOrNull(JsonElement parent, string name) =>
        parent.TryGetProperty(name, out var v) && v.ValueKind == JsonValueKind.Number
        && v.TryGetInt32(out var i)
            ? i
            : null;

    private static double DoubleOrZero(JsonElement parent, string name)
    {
        if (!parent.TryGetProperty(name, out var v))
        {
            return 0.0;
        }

        return v.ValueKind switch
        {
            JsonValueKind.Number when v.TryGetDouble(out var d) => d,
            JsonValueKind.String when double.TryParse(
                v.GetString(), NumberStyles.Float, CultureInfo.InvariantCulture, out var d) => d,
            _ => 0.0,
        };
    }

    private static string FirstNonEmpty(string a, string b) => a.Length > 0 ? a : b;
}
