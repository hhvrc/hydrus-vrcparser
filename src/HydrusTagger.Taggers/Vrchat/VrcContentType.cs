using System.Text.Json;
using System.Xml;
using System.Xml.Linq;

namespace HydrusTagger.Taggers.Vrchat;

/// <summary>
/// Classifies an iTXt chunk into one of the formats the VRChat tagger knows.
/// Port of <c>_detect_format</c> / <c>_is_xmp_xml</c> from
/// <c>core/png_itxt.py</c>.
/// </summary>
public static class VrcContentType
{
    public const string Json = "json";
    public const string Xml = "xml";
    public const string Line = "line";

    /// <summary>Unrecognized or non-VRChat content (GIMP comments, GameDVR, ...).</summary>
    public const string Text = "text";

    public const string DescriptionKeyword = "Description";
    public const string AdobeXmpKeyword = "XML:com.adobe.xmp";

    /// <summary>
    /// Detect the content type, or null when nothing recognizes it.
    /// </summary>
    /// <remarks>
    /// Order matters and matches the original: the XMP keyword short-circuits
    /// to xml regardless of content, then JSON, then XMP-shaped XML, then the
    /// legacy line format.
    /// </remarks>
    public static string? Detect(string? text, string? keyword = null)
    {
        if (string.Equals(keyword, AdobeXmpKeyword, StringComparison.Ordinal))
        {
            return Xml;
        }

        if (text is null)
        {
            return null;
        }

        if (IsJson(text))
        {
            return Json;
        }

        if (IsXmpXml(text))
        {
            return Xml;
        }

        try
        {
            MetaLineParser.Parse(text);
            return Line;
        }
        catch (MetaParseException)
        {
            // Not line format either.
        }

        return null;
    }

    /// <summary>
    /// Any valid JSON document counts, not just objects -- matching Python's
    /// <c>json.loads</c>, which accepts bare arrays and scalars too.
    /// </summary>
    public static bool IsJson(string text)
    {
        if (string.IsNullOrWhiteSpace(text))
        {
            return false;
        }

        try
        {
            using var _ = JsonDocument.Parse(text);
            return true;
        }
        catch (JsonException)
        {
            return false;
        }
    }

    /// <summary>
    /// True when the text parses as XML rooted at xmpmeta or a bare rdf:RDF.
    /// A namespace is required: the original compared against "}xmpmeta", which
    /// a namespace-less root can never match.
    /// </summary>
    public static bool IsXmpXml(string? text)
    {
        if (string.IsNullOrEmpty(text) || !text.TrimStart().StartsWith('<'))
        {
            return false;
        }

        try
        {
            var settings = new XmlReaderSettings
            {
                DtdProcessing = DtdProcessing.Prohibit,
                XmlResolver = null,
            };

            using var stringReader = new StringReader(text);
            using var reader = XmlReader.Create(stringReader, settings);
            var root = XDocument.Load(reader).Root;

            if (root is null || string.IsNullOrEmpty(root.Name.Namespace.NamespaceName))
            {
                return false;
            }

            return root.Name.LocalName is "xmpmeta" or "RDF";
        }
        catch (XmlException)
        {
            return false;
        }
    }
}
