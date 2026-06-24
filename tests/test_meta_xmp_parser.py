import unittest
from datetime import datetime, timezone, timedelta

from core.meta_xmp_parser import (
    parse_xmp_meta, XMPParseError, extract_embedded_vrcx_json, extract_editor_software,
)


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


EMBEDDED_VRCX_XMP = """\
<?xpacket begin="﻿" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="XMP Core 4.4.0-Exiv2">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:dc="http://purl.org/dc/elements/1.1/"
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
   xmp:CreatorTool="Adobe Photoshop Express (Android)">
   <dc:description>
    <rdf:Alt>
     <rdf:li xml:lang="x-default">{"application":"VRCX","version":1,\
"author":{"id":"usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd","displayName":"Project"},\
"world":{"name":"Wild Flower","id":"wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a",\
"instanceId":"wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a:60622~private"},\
"players":[{"id":"usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd","displayName":"Project"},\
{"id":"usr_c348aa66-98e5-4f64-96f3-62e7a14187d1","displayName":"ComfyHeaven"}]}</rdf:li>
    </rdf:Alt>
   </dc:description>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>"""


class TestEmbeddedVRCXJson(unittest.TestCase):
    """Adobe-edited VRChat screenshots wrap the original VRCX JSON inside the
    XMP's dc:description. parse_xmp_meta rejects them (no vrc: namespace), but
    extract_embedded_vrcx_json recovers the full payload."""

    def test_parse_xmp_meta_rejects(self):
        # No vrc: namespace identifiers → standard parser must reject
        with self.assertRaises(XMPParseError):
            parse_xmp_meta(EMBEDDED_VRCX_XMP)

    def test_extracts_world_id(self):
        data = extract_embedded_vrcx_json(EMBEDDED_VRCX_XMP)
        self.assertEqual(data["world"]["id"], "wrld_4be36a17-c43e-4e7a-bec3-ed35c414363a")

    def test_extracts_author_id(self):
        data = extract_embedded_vrcx_json(EMBEDDED_VRCX_XMP)
        self.assertEqual(data["author"]["id"], "usr_59ebd50f-bdf8-4ecf-a0ba-9d1788d92ecd")

    def test_extracts_players(self):
        data = extract_embedded_vrcx_json(EMBEDDED_VRCX_XMP)
        self.assertEqual(len(data["players"]), 2)

    def test_returns_none_for_plain_xmp(self):
        # XMP without embedded JSON should yield None, not raise
        self.assertIsNone(extract_embedded_vrcx_json(NON_VRCHAT_XMP))

    def test_returns_none_for_native_vrc_xmp(self):
        # vrc: namespace data lives in attributes/elements, not embedded JSON
        self.assertIsNone(extract_embedded_vrcx_json(NORMAL_XMP))

    def test_returns_none_for_invalid_xml(self):
        self.assertIsNone(extract_embedded_vrcx_json("not xml at all"))


class TestExtractEditorSoftware(unittest.TestCase):
    def test_creator_tool_as_attribute(self):
        # Adobe compact RDF form: CreatorTool + History softwareAgent as attrs
        agents = extract_editor_software(EMBEDDED_VRCX_XMP)
        self.assertIn("Adobe Photoshop Express (Android)", agents)

    def test_creator_tool_as_element(self):
        # Native VRChat XMP uses element form
        agents = extract_editor_software(NORMAL_XMP)
        self.assertEqual(agents, ["VRChat"])

    def test_history_software_agent(self):
        xml = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
           xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
           xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
    <rdf:Description>
      <xmp:CreatorTool>GIMP 2.10.34</xmp:CreatorTool>
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li stEvt:action="saved" stEvt:softwareAgent="Adobe Photoshop 25.0"/>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""
        agents = extract_editor_software(xml)
        self.assertIn("GIMP 2.10.34", agents)
        self.assertIn("Adobe Photoshop 25.0", agents)

    def test_dedup_preserves_order(self):
        xml = """\
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
           xmlns:xmp="http://ns.adobe.com/xap/1.0/"
           xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
           xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
    <rdf:Description>
      <xmp:CreatorTool>Adobe Photoshop Express (Android)</xmp:CreatorTool>
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li stEvt:softwareAgent="Adobe Photoshop Express (Android)"/>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>"""
        self.assertEqual(extract_editor_software(xml),
                         ["Adobe Photoshop Express (Android)"])

    def test_invalid_xml_returns_empty(self):
        self.assertEqual(extract_editor_software("not xml"), [])


if __name__ == "__main__":
    unittest.main()
