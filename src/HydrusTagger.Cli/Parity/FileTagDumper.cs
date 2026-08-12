using System.Text;
using System.Text.Encodings.Web;
using System.Text.Json;
using HydrusTagger.Core.Data;
using HydrusTagger.Core.Tagging;
using HydrusTagger.Taggers.Vrchat;
using Microsoft.EntityFrameworkCore;

namespace HydrusTagger.Cli.Parity;

/// <summary>
/// Dumps the tags the VRChat tagger derives for every file that has cached
/// iTXt chunks, so the whole pipeline -- priority contest, editor provenance,
/// tag building -- can be diffed against the Python's
/// <c>build_file_id_to_tags</c>.
/// </summary>
/// <remarks>
/// Per file rather than per chunk, unlike <see cref="ChunkParseDumper"/>: this
/// is the output that actually reaches Hydrus, and the only thing whose
/// equivalence makes the port safe to run against the live client.
/// </remarks>
internal static class FileTagDumper
{
    public static int Run(string databasePath, string outputPath)
    {
        var options = new DbContextOptionsBuilder<TaggerDbContext>()
            .UseSqlite(DataServiceCollectionExtensions.BuildConnectionString(databasePath))
            .Options;

        using var db = new TaggerDbContext(options);

        var chunksByFile = db.ItxtChunks.AsNoTracking()
            .OrderBy(c => c.FileId).ThenBy(c => c.Seq)
            .Select(c => new { c.FileId, c.Keyword, c.Text, c.ContentType })
            .AsEnumerable()
            .GroupBy(c => c.FileId);

        var jsonOptions = new JsonSerializerOptions
        {
            // Match Python's ensure_ascii=False so the diff compares characters
            // rather than differing escape conventions.
            Encoder = JavaScriptEncoder.UnsafeRelaxedJsonEscaping,
        };

        var written = 0;
        var tagged = 0;
        using var output = new StreamWriter(outputPath, append: false, new UTF8Encoding(false));

        foreach (var group in chunksByFile)
        {
            var meta = VrchatMetaLoader.Load(
                group.Select(c => new VrcChunk(c.Keyword, c.Text, c.ContentType)));

            written++;
            if (meta is null)
            {
                // The Python simply has no entry for such a file. Recording the
                // absence explicitly keeps the two dumps aligned on keys.
                output.WriteLine(JsonSerializer.Serialize(
                    new Record(group.Key, null, null), jsonOptions));
                continue;
            }

            var tags = new TagSet(VrchatTagBuilder.BuildFileTags(meta));
            tagged++;
            output.WriteLine(JsonSerializer.Serialize(
                new Record(group.Key, [.. tags.SortedTags], tags.Hash), jsonOptions));
        }

        Console.WriteLine($"wrote {written} file records ({tagged} with tags) to {outputPath}");
        return 0;
    }

    private sealed record Record(int file_id, List<string>? tags, string? tag_hash);
}
