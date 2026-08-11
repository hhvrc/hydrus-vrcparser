using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using HydrusTagger.Core.Data;
using HydrusTagger.Core.Text;
using HydrusTagger.Taggers.Vrchat;
using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Cli.Parity;

/// <summary>
/// Dumps per-chunk parse results for every cached iTXt chunk, in the same
/// shape as the Python reference dump, so the two can be diffed line for line.
/// </summary>
/// <remarks>
/// Operates one chunk at a time on purpose: it isolates parser behaviour from
/// the file-level priority contest, so a mismatch points at a specific parser
/// rather than at the dispatch logic.
/// </remarks>
internal static class ChunkParseDumper
{
    public static int Run(string databasePath, string outputPath)
    {
        var options = new DbContextOptionsBuilder<TaggerDbContext>()
            .UseSqlite(DataServiceCollectionExtensions.BuildConnectionString(databasePath))
            .Options;

        using var db = new TaggerDbContext(options);

        var chunks = db.ItxtChunks.AsNoTracking()
            .OrderBy(c => c.FileId).ThenBy(c => c.Seq)
            .Select(c => new { c.FileId, c.Seq, c.Keyword, c.Text, c.ContentType })
            .AsEnumerable();

        var jsonOptions = new JsonSerializerOptions
        {
            // Match Python's ensure_ascii=False so the diff compares characters,
            // not differing escape conventions.
            Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
        };

        var written = 0;
        using var output = new StreamWriter(outputPath, append: false, new UTF8Encoding(false));

        foreach (var chunk in chunks)
        {
            var rawText = TextSanitizer.SanitizeItxt(chunk.Text);
            var storedType = (chunk.ContentType ?? "").ToLowerInvariant();

            // Anything not json/xml is attempted as line format.
            var ctype = storedType is VrcContentType.Json or VrcContentType.Xml
                ? storedType
                : VrcContentType.Line;

            var record = new SortedDictionary<string, object?>(StringComparer.Ordinal)
            {
                ["file_id"] = chunk.FileId,
                ["seq"] = chunk.Seq,
                ["keyword"] = chunk.Keyword,
                ["stored_type"] = chunk.ContentType,
            };

            try
            {
                var (meta, editor, effective) = ParseChunk(rawText, ctype);

                record["effective_type"] = effective;
                record["author_id"] = meta.Author.Id;
                record["author_name"] = meta.Author.DisplayName;
                record["world_id"] = meta.World.Id;
                record["world_name"] = meta.World.Name;
                record["instance_id"] = meta.World.InstanceId;
                record["creator_tool"] = meta.CreatorTool;
                record["editor_software"] = editor;
                record["created"] = FormatCreated(meta.Created);
                record["players"] = meta.Players
                    .Select(p => new[] { p.Id, p.DisplayName })
                    .ToList();
            }
            catch (Exception ex) when (ex is MetaParseException or XmpParseException or JsonException)
            {
                record["error"] = MapExceptionName(ex);
            }

            output.WriteLine(JsonSerializer.Serialize(record, jsonOptions));
            written++;
        }

        Console.WriteLine($"wrote {written} chunk records to {outputPath}");
        return 0;
    }

    private static (VrcMetadata Meta, List<string> Editor, string Effective) ParseChunk(
        string rawText, string ctype)
    {
        List<string> editor = [];
        var effective = ctype;
        VrcMetadata meta;

        switch (ctype)
        {
            case VrcContentType.Json:
                using (var doc = JsonDocument.Parse(rawText))
                {
                    meta = MetaNormalizer.FromJson(doc.RootElement, rawText);
                }

                break;

            case VrcContentType.Xml:
                try
                {
                    meta = MetaNormalizer.FromXmp(XmpMetaParser.Parse(rawText), rawText);
                }
                catch (XmpParseException)
                {
                    // Adobe-resaved screenshots keep the VRCX JSON in dc:description.
                    var embedded = XmpMetaParser.ExtractEmbeddedVrcxJson(rawText);
                    if (embedded is not null)
                    {
                        meta = MetaNormalizer.FromJson(embedded.Value, rawText);
                        effective = VrcContentType.Json;
                    }
                    else
                    {
                        meta = MetaLineParser.Parse(rawText);
                        effective = VrcContentType.Line;
                    }
                }

                editor = XmpMetaParser.ExtractEditorSoftware(rawText);
                break;

            default:
                meta = MetaLineParser.Parse(rawText);
                effective = VrcContentType.Line;
                break;
        }

        return (meta, editor, effective);
    }

    /// <summary>Render like Python's <c>datetime.isoformat()</c> for comparison.</summary>
    private static string? FormatCreated(DateTimeOffset? created)
    {
        if (created is null)
        {
            return null;
        }

        var v = created.Value;
        var fractional = v.Ticks % TimeSpan.TicksPerSecond == 0 ? "" : ".ffffff";
        return v.ToString($"yyyy-MM-ddTHH:mm:ss{fractional}zzz", System.Globalization.CultureInfo.InvariantCulture);
    }

    /// <summary>Map to the Python exception name the reference dump records.</summary>
    private static string MapExceptionName(Exception ex) => ex switch
    {
        MetaParseException => "MetaParseError",
        XmpParseException => "XMPParseError",
        JsonException => "JSONDecodeError",
        _ => ex.GetType().Name,
    };
}
