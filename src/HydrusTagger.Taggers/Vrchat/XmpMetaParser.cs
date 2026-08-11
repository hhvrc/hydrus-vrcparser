using System.Globalization;
using System.Text.Json;
using System.Text.RegularExpressions;
using System.Xml;
using System.Xml.Linq;

namespace HydrusTagger.Taggers.Vrchat;

public sealed class XmpParseException : Exception
{
    public XmpParseException(string message) : base(message) { }
    public XmpParseException(string message, Exception inner) : base(message, inner) { }
}

/// <summary>Typed result of a native VRChat XMP packet.</summary>
public sealed class XmpMetadata
{
    public string RawXml { get; set; } = "";
    public string? CreatorTool { get; set; }
    public string? AuthorId { get; set; }
    public string? AuthorDisplayName { get; set; }
    public DateTimeOffset? Created { get; set; }
    public DateTimeOffset? Modified { get; set; }
    public DateTimeOffset? TiffDateTime { get; set; }
    public string? WorldId { get; set; }
    public string? WorldName { get; set; }
}

/// <summary>Port of <c>core/meta_xmp_parser.py</c>.</summary>
public static partial class XmpMetaParser
{
    private static readonly XNamespace X = "adobe:ns:meta/";
    private static readonly XNamespace Rdf = "http://www.w3.org/1999/02/22-rdf-syntax-ns#";
    private static readonly XNamespace Xmp = "http://ns.adobe.com/xap/1.0/";
    private static readonly XNamespace Tiff = "http://ns.adobe.com/tiff/1.0/";
    private static readonly XNamespace Vrc = "http://ns.vrchat.com/vrc/1.0/";

    [GeneratedRegex(
        @"^usr_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")]
    private static partial Regex UserIdPattern { get; }

    [GeneratedRegex(
        @"^wrld_[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")]
    private static partial Regex WorldIdPattern { get; }

    private static string? ValidUserId(string? s) => s is not null && UserIdPattern.IsMatch(s) ? s : null;

    private static string? ValidWorldId(string? s) => s is not null && WorldIdPattern.IsMatch(s) ? s : null;

    /// <summary>
    /// The element's own leading text, equivalent to ElementTree's
    /// <c>.text</c>: everything between the start tag and the first child
    /// element, or null when a child element comes first.
    /// </summary>
    /// <remarks>
    /// Deliberately not <see cref="XElement.Value"/>, which concatenates all
    /// descendant text. For a nested node such as
    /// <c>&lt;dc:description&gt;&lt;rdf:Alt&gt;...&lt;/rdf:Alt&gt;&lt;/dc:description&gt;</c>
    /// the Python original sees no text at all, and so must we.
    /// </remarks>
    private static string? DirectText(XElement e)
    {
        string? result = null;
        foreach (var node in e.Nodes())
        {
            if (node is XText t)
            {
                result += t.Value;
            }
            else
            {
                break;
            }
        }

        return result;
    }

    /// <summary>
    /// Parse a VRChat XMP packet.
    /// </summary>
    /// <remarks>
    /// Two forms exist. In the <em>normal</em> form the VRC namespace carries
    /// WorldID / WorldDisplayName / AuthorID, and xmp:Author is a human-readable
    /// name. In the <em>compact</em> form only vrc:World is present, and
    /// xmp:Author may itself be a usr_ id. Author is only ever interpreted as an
    /// id when the compact form is detected.
    /// </remarks>
    /// <exception cref="XmpParseException">
    /// Structurally invalid, or carrying no usable VRChat identifier.
    /// </exception>
    public static XmpMetadata Parse(string xmlText)
    {
        var grouped = ParseOuter(xmlText);

        string? TextOf(XNamespace ns, string local) =>
            grouped.TryGetValue(ns.NamespaceName, out var bucket)
            && bucket.TryGetValue(local, out var element)
                ? (DirectText(element) ?? "").Trim()
                : null;

        var creatorTool = TextOf(Xmp, "CreatorTool");
        var rawAuthor = TextOf(Xmp, "Author");

        string? worldId = null;
        string? worldName = null;
        string? authorId = null;
        string? authorName;

        var widVal = TextOf(Vrc, "WorldID");
        if (!string.IsNullOrEmpty(widVal))
        {
            worldId = ValidWorldId(widVal) ?? worldId;
        }

        var wnameVal = TextOf(Vrc, "WorldDisplayName");
        if (!string.IsNullOrEmpty(wnameVal))
        {
            worldName = wnameVal;
        }

        var aidVal = TextOf(Vrc, "AuthorID");
        if (!string.IsNullOrEmpty(aidVal))
        {
            authorId = ValidUserId(aidVal) ?? authorId;
        }

        // Note: presence, not truthiness -- an empty <vrc:World/> still marks
        // the compact form.
        var worldCompactVal = TextOf(Vrc, "World");
        var compactWorldPresent = worldCompactVal is not null;
        if (compactWorldPresent)
        {
            worldId = ValidWorldId(worldCompactVal) ?? worldId;
        }

        if (compactWorldPresent)
        {
            if (!string.IsNullOrEmpty(rawAuthor) && ValidUserId(rawAuthor) is not null)
            {
                // An explicit vrc:AuthorID wins over an inferred one.
                authorId ??= rawAuthor;
                authorName = null;
            }
            else
            {
                authorName = rawAuthor;
            }
        }
        else
        {
            authorName = rawAuthor;
        }

        if (worldId is null && authorId is null)
        {
            throw new XmpParseException("Missing or invalid VRChat identifiers in XMP metadata");
        }

        return new XmpMetadata
        {
            RawXml = xmlText,
            CreatorTool = creatorTool,
            AuthorId = authorId,
            AuthorDisplayName = authorName,
            Created = ParseDateTime(TextOf(Xmp, "CreateDate")),
            Modified = ParseDateTime(TextOf(Xmp, "ModifyDate")),
            TiffDateTime = ParseDateTime(TextOf(Tiff, "DateTime")),
            WorldId = worldId,
            WorldName = worldName,
        };
    }

    /// <summary>
    /// Validate the envelope and group rdf:Description children by namespace
    /// and local name.
    /// </summary>
    private static Dictionary<string, Dictionary<string, XElement>> ParseOuter(string xmlText)
    {
        XDocument doc;
        try
        {
            doc = LoadXml(xmlText);
        }
        catch (XmlException ex)
        {
            throw new XmpParseException($"XML parsing failed: {ex.Message}", ex);
        }

        var root = doc.Root ?? throw new XmpParseException("XML parsing failed: empty document");

        if (root.Name.Namespace != X || root.Name.LocalName != "xmpmeta")
        {
            throw new XmpParseException($"Invalid root element: expected {{{X.NamespaceName}}}xmpmeta");
        }

        var rdfChildren = root.Elements().Where(c => c.Name.Namespace == Rdf && c.Name.LocalName == "RDF").ToList();
        if (rdfChildren.Count != 1)
        {
            throw new XmpParseException("Expected exactly one rdf:RDF child element");
        }

        var grouped = new Dictionary<string, Dictionary<string, XElement>>(StringComparer.Ordinal);

        foreach (var desc in rdfChildren[0].Elements())
        {
            if (desc.Name.Namespace != Rdf || desc.Name.LocalName != "Description")
            {
                continue;
            }

            foreach (var elem in desc.Elements())
            {
                var ns = elem.Name.Namespace.NamespaceName;
                if (string.IsNullOrEmpty(ns))
                {
                    // Elements without a namespace are not addressable here.
                    continue;
                }

                if (!grouped.TryGetValue(ns, out var bucket))
                {
                    bucket = new Dictionary<string, XElement>(StringComparer.Ordinal);
                    grouped[ns] = bucket;
                }

                if (!bucket.TryAdd(elem.Name.LocalName, elem))
                {
                    throw new XmpParseException(
                        $"Duplicate element for {{{ns}}}{elem.Name.LocalName} in XMP metadata");
                }
            }
        }

        return grouped;
    }

    /// <summary>
    /// Recover VRCX JSON embedded in an XMP packet.
    /// </summary>
    /// <remarks>
    /// VRChat screenshots re-saved by Adobe apps lose the vrc: namespace but
    /// keep the original VRCX JSON inside dc:description, typically as
    /// <c>&lt;dc:description&gt;&lt;rdf:Alt&gt;&lt;rdf:li&gt;{...}&lt;/rdf:li&gt;</c>.
    /// <see cref="Parse"/> rejects those packets, so this recovers the payload
    /// for the standard JSON path. Returns null when nothing is embedded.
    /// </remarks>
    public static JsonElement? ExtractEmbeddedVrcxJson(string xmlText)
    {
        XDocument doc;
        try
        {
            doc = LoadXml(xmlText);
        }
        catch (XmlException)
        {
            return null;
        }

        if (doc.Root is null)
        {
            return null;
        }

        foreach (var elem in doc.Root.DescendantsAndSelf())
        {
            var txt = (DirectText(elem) ?? "").Trim();

            // Cheap pre-filter before attempting a decode on every text node.
            if (txt.Length == 0 || txt[0] != '{')
            {
                continue;
            }

            if (!txt.Contains("\"world\"", StringComparison.Ordinal)
                && !txt.Contains("\"author\"", StringComparison.Ordinal))
            {
                continue;
            }

            JsonDocument parsed;
            try
            {
                parsed = JsonDocument.Parse(txt);
            }
            catch (JsonException)
            {
                continue;
            }

            var root = parsed.RootElement;
            if (root.ValueKind == JsonValueKind.Object && (HasContent(root, "world") || HasContent(root, "author")))
            {
                return root.Clone();
            }
        }

        return null;
    }

    /// <summary>Truthiness test matching Python's <c>data.get(key)</c>.</summary>
    private static bool HasContent(JsonElement obj, string key) =>
        obj.TryGetProperty(key, out var v)
        && v.ValueKind switch
        {
            JsonValueKind.Null or JsonValueKind.False or JsonValueKind.Undefined => false,
            JsonValueKind.String => v.GetString()?.Length > 0,
            JsonValueKind.Array => v.GetArrayLength() > 0,
            JsonValueKind.Object => v.EnumerateObject().Any(),
            JsonValueKind.Number => v.TryGetDouble(out var d) && d != 0,
            _ => true,
        };

    /// <summary>
    /// Names of software that created or edited the image: xmp:CreatorTool plus
    /// every stEvt:softwareAgent in the xmpMM:History log. Both appear as
    /// elements (native VRChat XMP) or attributes (Adobe's compact RDF), so
    /// match by local name across both. Order-preserving and de-duplicated.
    /// </summary>
    public static List<string> ExtractEditorSoftware(string xmlText)
    {
        var found = new List<string>();

        XDocument doc;
        try
        {
            doc = LoadXml(xmlText);
        }
        catch (XmlException)
        {
            return found;
        }

        if (doc.Root is null)
        {
            return found;
        }

        void Add(string? value)
        {
            var v = (value ?? "").Trim();
            if (v.Length > 0 && !found.Contains(v, StringComparer.Ordinal))
            {
                found.Add(v);
            }
        }

        foreach (var elem in doc.Root.DescendantsAndSelf())
        {
            if (elem.Name.LocalName is "CreatorTool" or "softwareAgent")
            {
                Add(DirectText(elem));
            }

            foreach (var attr in elem.Attributes())
            {
                if (attr.Name.LocalName is "CreatorTool" or "softwareAgent")
                {
                    Add(attr.Value);
                }
            }
        }

        return found;
    }

    /// <summary>
    /// Parse an XMP timestamp, preserving its original UTC offset.
    /// </summary>
    /// <remarks>
    /// The offset matters: the date tag is rendered from this value, so
    /// normalizing to UTC could shift a late-evening screenshot onto the
    /// previous day. Offset-less values are treated as UTC, matching
    /// <c>_parse_dt</c>.
    /// </remarks>
    internal static DateTimeOffset? ParseDateTime(string? value)
    {
        if (string.IsNullOrWhiteSpace(value))
        {
            return null;
        }

        var s = value.Trim();
        if (s.EndsWith('Z'))
        {
            s = s[..^1] + "+00:00";
        }

        // AssumeUniversal applies only when the string carries no offset;
        // without AdjustToUniversal an explicit offset is preserved as written.
        return DateTimeOffset.TryParse(
            s, CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var dto)
            ? dto
            : null;
    }

    /// <summary>
    /// Load XML with DTD processing disabled and no external resolution --
    /// these packets come from arbitrary image files.
    /// </summary>
    private static XDocument LoadXml(string xmlText)
    {
        var settings = new XmlReaderSettings
        {
            DtdProcessing = DtdProcessing.Prohibit,
            XmlResolver = null,
            IgnoreWhitespace = false,
        };

        using var stringReader = new StringReader(xmlText);
        using var reader = XmlReader.Create(stringReader, settings);
        return XDocument.Load(reader);
    }
}
