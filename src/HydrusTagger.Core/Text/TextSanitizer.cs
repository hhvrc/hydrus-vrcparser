namespace HydrusTagger.Core.Text;

public static class TextSanitizer
{
    /// <summary>
    /// Clean raw iTXt text: drop leading NULs some exporters prepend, drop a
    /// UTF-8 BOM, then trim. Port of <c>core/utils.py:sanitize_itxt_text</c>.
    /// </summary>
    /// <remarks>
    /// Deliberately does NOT attempt to repair mis-encoded text. Real stored
    /// chunks contain double-encoded UTF-8 in display names (e.g. "Night&#226;&#136;&#151;");
    /// those bytes are part of the tag value and "fixing" them here would
    /// change every tag derived from them.
    /// </remarks>
    public static string SanitizeItxt(string? text)
    {
        if (string.IsNullOrEmpty(text))
        {
            return "";
        }

        var t = text.TrimStart('\0');
        if (t.StartsWith('﻿'))
        {
            t = t.TrimStart('﻿');
        }

        return t.Trim();
    }
}
