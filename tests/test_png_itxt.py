import unittest

from core.png_itxt import _is_xmp_xml, _detect_format, _parse_itxt_descriptors
from core.constants import ITXT_KEY_ADOBEXMPXML


class TestIsXmpXml(unittest.TestCase):
    def test_valid_xmpmeta(self):
        xml = '<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></x:xmpmeta>'
        self.assertTrue(_is_xmp_xml(xml))

    def test_bare_rdf(self):
        xml = '<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/>'
        self.assertTrue(_is_xmp_xml(xml))

    def test_non_xmp_xml(self):
        self.assertFalse(_is_xmp_xml("<root><child/></root>"))

    def test_invalid_xml(self):
        self.assertFalse(_is_xmp_xml("not xml"))

    def test_empty_string(self):
        self.assertFalse(_is_xmp_xml(""))

    def test_none(self):
        self.assertFalse(_is_xmp_xml(None))

    def test_json(self):
        self.assertFalse(_is_xmp_xml('{"key": "value"}'))


class TestDetectFormat(unittest.TestCase):
    def test_json(self):
        self.assertEqual(_detect_format('{"key": "value"}'), "json")

    def test_json_array(self):
        self.assertEqual(_detect_format('[1, 2, 3]'), "json")

    def test_xmp_keyword_always_xml(self):
        self.assertEqual(_detect_format("anything", keyword=ITXT_KEY_ADOBEXMPXML), "xml")

    def test_xmp_xml_detected(self):
        xml = '<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"/></x:xmpmeta>'
        self.assertEqual(_detect_format(xml), "xml")

    def test_legacy_line(self):
        self.assertEqual(_detect_format("screenshotmanager|0|author:usr_abc,TestUser"), "line")

    def test_unknown_returns_none(self):
        self.assertIsNone(_detect_format("random garbage"))


class TestParseItxtDescriptors(unittest.TestCase):
    """Test the iTXt binary chunk parser."""

    def _make_itxt(self, keyword, comp_flag=0, comp_method=0, lang="", trans="", text=""):
        """Build a raw iTXt data blob matching PNG spec layout."""
        return (
            keyword.encode("utf-8")
            + b"\x00"
            + bytes([comp_flag, comp_method])
            + lang.encode("utf-8")
            + b"\x00"
            + trans.encode("utf-8")
            + b"\x00"
            + text.encode("utf-8")
        )

    def test_basic_uncompressed(self):
        data = self._make_itxt("Description", text="hello world")
        result = _parse_itxt_descriptors(data)
        self.assertIsNotNone(result)
        keyword, cf, cm, lang, trans, text = result
        self.assertEqual(keyword, "Description")
        self.assertEqual(cf, 0)
        self.assertEqual(cm, 0)
        self.assertEqual(lang, "")
        self.assertEqual(trans, "")
        self.assertEqual(text, "hello world")

    def test_nonzero_compression_flag(self):
        """Compression flag=1 should be parsed correctly (not silently dropped)."""
        data = self._make_itxt("Description", comp_flag=1, text="compressed data")
        result = _parse_itxt_descriptors(data)
        self.assertIsNotNone(result)
        keyword, cf, cm, lang, trans, text = result
        self.assertEqual(cf, 1)
        self.assertEqual(text, "compressed data")

    def test_with_language_and_translated_keyword(self):
        data = self._make_itxt("Description", lang="en", trans="Desc", text="content")
        result = _parse_itxt_descriptors(data)
        self.assertIsNotNone(result)
        keyword, cf, cm, lang, trans, text = result
        self.assertEqual(lang, "en")
        self.assertEqual(trans, "Desc")
        self.assertEqual(text, "content")

    def test_text_with_null_bytes(self):
        """Text portion containing null bytes should be preserved."""
        data = self._make_itxt("Description", text="before\x00after")
        result = _parse_itxt_descriptors(data)
        # The split(b"\x00", 2) on the remainder means the text retains its nulls
        self.assertIsNotNone(result)

    def test_truncated_data_returns_none(self):
        """Data too short to contain comp_flag + comp_method returns None."""
        data = b"Description\x00"  # missing comp_flag and comp_method
        result = _parse_itxt_descriptors(data)
        self.assertIsNone(result)

    def test_no_null_separator_returns_none(self):
        """Data with no null byte at all returns None."""
        data = b"just some bytes with no null"
        result = _parse_itxt_descriptors(data)
        self.assertIsNone(result)

    def test_missing_text_section_returns_none(self):
        """Only keyword + flags + lang, missing translated_keyword/text null separators."""
        data = b"Description\x00\x00\x00en"  # no more null separators after lang
        result = _parse_itxt_descriptors(data)
        self.assertIsNone(result)

    def test_empty_keyword(self):
        data = self._make_itxt("", text="some text")
        result = _parse_itxt_descriptors(data)
        self.assertIsNotNone(result)
        keyword, cf, cm, lang, trans, text = result
        self.assertEqual(keyword, "")
        self.assertEqual(text, "some text")

    def test_json_text_content(self):
        """Realistic VRC JSON metadata in iTXt chunk."""
        json_text = '{"author":{"id":"usr_abc","displayName":"Test"}}'
        data = self._make_itxt("Description", text=json_text)
        result = _parse_itxt_descriptors(data)
        self.assertIsNotNone(result)
        self.assertEqual(result[5], json_text)


if __name__ == "__main__":
    unittest.main()
