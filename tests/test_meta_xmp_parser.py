import unittest
from datetime import datetime, timezone, timedelta

from core.meta_xmp_parser import parse_xmp_meta, XMPParseError


NORMAL_XMP = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>TestUser</xmp:Author>
      <xmp:CreateDate>2025-08-30T06:45:33+02:00</xmp:CreateDate>
      <xmp:ModifyDate>2025-08-30T06:45:33+02:00</xmp:ModifyDate>
    </rdf:Description>
    <rdf:Description xmlns:tiff="http://ns.adobe.com/tiff/1.0/">
      <tiff:DateTime>2025-08-30T06:45:33+02:00</tiff:DateTime>
    </rdf:Description>
    <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
      <vrc:WorldDisplayName>Test World</vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""

COMPACT_XMP = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</xmp:Author>
    </rdf:Description>
    <rdf:Description xmlns:tiff="http://ns.adobe.com/tiff/1.0/">
      <tiff:DateTime />
    </rdf:Description>
    <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:World>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:World>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""

EMPTY_WORLD_XMP = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>SomeUser</xmp:Author>
      <xmp:CreateDate>2025-08-30T02:15:48+02:00</xmp:CreateDate>
      <xmp:ModifyDate>2025-08-30T02:15:48+02:00</xmp:ModifyDate>
    </rdf:Description>
    <rdf:Description xmlns:tiff="http://ns.adobe.com/tiff/1.0/">
      <tiff:DateTime>2025-08-30T02:15:48+02:00</tiff:DateTime>
    </rdf:Description>
    <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID />
      <vrc:WorldDisplayName></vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""

NON_VRCHAT_XMP = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="XMP Core 5.5.0">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
   xmp:ModifyDate="2025-07-22T17:39:09+02:00">
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>"""

RESAVED_PHOTO_VIEWER_XMP = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>Microsoft Windows Photo Viewer 10.0.26100.1882</xmp:CreatorTool>
      <xmp:Author>TestUser</xmp:Author>
      <xmp:CreateDate>2025-08-30T06:45:33+02:00</xmp:CreateDate>
      <xmp:ModifyDate>2025-09-01T12:00:00+02:00</xmp:ModifyDate>
    </rdf:Description>
    <rdf:Description xmlns:tiff="http://ns.adobe.com/tiff/1.0/">
      <tiff:DateTime>2025-09-01T12:00:00+02:00</tiff:DateTime>
    </rdf:Description>
    <rdf:Description xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:title><rdf:Alt><rdf:li xml:lang="x-default"></rdf:li></rdf:Alt></dc:title>
    </rdf:Description>
    <rdf:Description xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">
      <vrc:WorldID>wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>
      <vrc:WorldDisplayName>Test World</vrc:WorldDisplayName>
      <vrc:AuthorID>usr_56e86082-c91c-40a4-bb92-2486ceca90eb</vrc:AuthorID>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""


class TestNormalForm(unittest.TestCase):
    def setUp(self):
        self.result = parse_xmp_meta(NORMAL_XMP)

    def test_type(self):
        self.assertEqual(self.result["type"], "xmp")

    def test_creator_tool(self):
        self.assertEqual(self.result["creator_tool"], "VRChat")

    def test_author_id(self):
        self.assertEqual(self.result["author"]["id"], "usr_56e86082-c91c-40a4-bb92-2486ceca90eb")

    def test_author_name(self):
        self.assertEqual(self.result["author"]["displayName"], "TestUser")

    def test_world_id(self):
        self.assertEqual(self.result["world"]["id"], "wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe")

    def test_world_name(self):
        self.assertEqual(self.result["world"]["name"], "Test World")

    def test_created_date(self):
        expected = datetime(2025, 8, 30, 6, 45, 33, tzinfo=timezone(timedelta(hours=2)))
        self.assertEqual(self.result["created"], expected)


class TestCompactForm(unittest.TestCase):
    def setUp(self):
        self.result = parse_xmp_meta(COMPACT_XMP)

    def test_author_id_from_xmp_author(self):
        self.assertEqual(self.result["author"]["id"], "usr_56e86082-c91c-40a4-bb92-2486ceca90eb")

    def test_author_name_none(self):
        self.assertIsNone(self.result["author"]["displayName"])

    def test_world_id(self):
        self.assertEqual(self.result["world"]["id"], "wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe")


class TestEmptyWorldID(unittest.TestCase):
    def setUp(self):
        self.result = parse_xmp_meta(EMPTY_WORLD_XMP)

    def test_author_id(self):
        self.assertEqual(self.result["author"]["id"], "usr_56e86082-c91c-40a4-bb92-2486ceca90eb")

    def test_world_id_none(self):
        self.assertIsNone(self.result["world"]["id"])

    def test_author_name_preserved(self):
        self.assertEqual(self.result["author"]["displayName"], "SomeUser")


class TestNonVRChatXMP(unittest.TestCase):
    def test_rejects_non_vrchat(self):
        with self.assertRaises(XMPParseError):
            parse_xmp_meta(NON_VRCHAT_XMP)

    def test_rejects_invalid_xml(self):
        with self.assertRaises(XMPParseError):
            parse_xmp_meta("not xml at all")

    def test_rejects_wrong_root(self):
        with self.assertRaises(XMPParseError):
            parse_xmp_meta("<root><child/></root>")

    def test_rejects_no_identifiers(self):
        xml = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
    <rdf:Description>
      <xmp:CreatorTool>VRChat</xmp:CreatorTool>
      <xmp:Author>NoIDs</xmp:Author>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""
        with self.assertRaises(XMPParseError):
            parse_xmp_meta(xml)


class TestResavedPhotoViewerXMP(unittest.TestCase):
    """Files re-saved in Windows Photo Viewer preserve VRC namespace data
    but have CreatorTool overwritten. Parser should accept these."""

    def setUp(self):
        self.result = parse_xmp_meta(RESAVED_PHOTO_VIEWER_XMP)

    def test_creator_tool_preserved(self):
        self.assertEqual(self.result["creator_tool"],
                         "Microsoft Windows Photo Viewer 10.0.26100.1882")

    def test_author_id(self):
        self.assertEqual(self.result["author"]["id"],
                         "usr_56e86082-c91c-40a4-bb92-2486ceca90eb")

    def test_author_name(self):
        self.assertEqual(self.result["author"]["displayName"], "TestUser")

    def test_world_id(self):
        self.assertEqual(self.result["world"]["id"],
                         "wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe")

    def test_world_name(self):
        self.assertEqual(self.result["world"]["name"], "Test World")

    def test_type(self):
        self.assertEqual(self.result["type"], "xmp")


if __name__ == "__main__":
    unittest.main()
