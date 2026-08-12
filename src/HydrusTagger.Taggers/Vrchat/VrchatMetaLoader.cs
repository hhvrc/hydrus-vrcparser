using System.Text.Json;
using HydrusTagger.Core.Text;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>One cached iTXt chunk, as the loader needs it.</summary>
public sealed record VrcChunk(string? Keyword, string? Text, string? ContentType);

/// <summary>
/// Resolves one file's cached iTXt chunks into a single normalized metadata
/// record. Port of <c>db_logic.py:db_load_all_parsed_meta</c>, narrowed from
/// the whole table to one file -- the priority contest was always per file.
/// </summary>
public static class VrchatMetaLoader
{
    /// <summary>
    /// A richer VRCX JSON payload beats an XMP packet, which beats the legacy
    /// pipe-delimited line.
    /// </summary>
    private static int Priority(string contentType) => contentType switch
    {
        VrcContentType.Json => 3,
        VrcContentType.Xml => 2,
        _ => 1,
    };

    /// <summary>
    /// Best metadata for the file, or null if no chunk yielded any.
    /// </summary>
    public static VrcMetadata? Load(IEnumerable<VrcChunk> chunks)
    {
        ArgumentNullException.ThrowIfNull(chunks);

        VrcMetadata? best = null;
        var bestType = "";

        // Editor provenance is orthogonal to the priority contest: a file's
        // VRCX JSON may win on priority while the editor software lives in a
        // separate XMP chunk. Collect it independently and merge at the end.
        var editorSoftware = new List<string>();

        foreach (var chunk in chunks)
        {
            if (chunk.Keyword is not (VrcContentType.DescriptionKeyword or VrcContentType.AdobeXmpKeyword))
            {
                continue;
            }

            var rawText = TextSanitizer.SanitizeItxt(chunk.Text);
            var contentType = (chunk.ContentType ?? "").ToLowerInvariant();

            if (contentType.Length == 0 && VrcContentType.IsXmpXml(rawText))
            {
                contentType = VrcContentType.Xml;
            }

            if (contentType is not (VrcContentType.Json or VrcContentType.Xml))
            {
                contentType = VrcContentType.Line;
            }

            VrcMetadata meta;
            try
            {
                (meta, contentType) = ParseChunk(rawText, contentType, editorSoftware);
            }
            catch (Exception ex) when (ex is JsonException or MetaParseException or XmpParseException
                                          or FormatException or KeyNotFoundException)
            {
                // Irreparably broken chunk. Another chunk on the same file may
                // still parse, so this is a continue rather than a failure.
                continue;
            }

            if (best is not null && Priority(contentType) <= Priority(bestType))
            {
                continue;
            }

            best = meta;
            bestType = contentType;
        }

        if (best is not null && editorSoftware.Count > 0)
        {
            best.EditorSoftware = editorSoftware;
        }

        return best;
    }

    /// <summary>
    /// Parse one chunk, returning the content type it turned out to be -- an
    /// XMP packet can resolve to JSON or line, and that changes its priority.
    /// </summary>
    private static (VrcMetadata Meta, string EffectiveType) ParseChunk(
        string rawText, string contentType, List<string> editorSoftware)
    {
        switch (contentType)
        {
            case VrcContentType.Json:
            {
                using var doc = JsonDocument.Parse(rawText);
                return (MetaNormalizer.FromJson(doc.RootElement, rawText), VrcContentType.Json);
            }

            case VrcContentType.Xml:
            {
                VrcMetadata meta;
                var effective = VrcContentType.Xml;

                try
                {
                    meta = MetaNormalizer.FromXmp(XmpMetaParser.Parse(rawText), rawText);
                }
                catch (XmpParseException)
                {
                    // Adobe-edited VRChat screenshots wrap the original VRCX
                    // JSON inside dc:description; recover it if present.
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

                // Recorded even when the chunk loses the priority contest --
                // that is the point of tracking it separately.
                foreach (var software in XmpMetaParser.ExtractEditorSoftware(rawText))
                {
                    if (!editorSoftware.Contains(software, StringComparer.Ordinal))
                    {
                        editorSoftware.Add(software);
                    }
                }

                return (meta, effective);
            }

            default:
                return (MetaLineParser.Parse(rawText), VrcContentType.Line);
        }
    }
}
