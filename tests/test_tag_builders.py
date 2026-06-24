import unittest
from datetime import datetime, timezone, timedelta

from core.tag_builders import build_tag_mappings, build_file_id_to_tags, build_editor_tags


class TestBuildFileIdToTags(unittest.TestCase):
    def test_basic_tags(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "TestUser"},
                "world": {"id": "wrld_xyz", "instanceId": "", "name": "MyWorld"},
                "players": [],
            }
        }
        result = build_file_id_to_tags(meta)
        tags = result[1]
        self.assertIn("vrchat", tags)
        self.assertIn("vrchat-author-id:usr_abc", tags)
        self.assertIn("vrchat-author-name:TestUser", tags)
        self.assertIn("vrchat-world-id:wrld_xyz", tags)
        self.assertIn("vrchat-world-name:MyWorld", tags)

    def test_creator_tool_tag(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
                "creator_tool": "VRChat",
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertIn("creator_tool:VRChat", tags)

    def test_creator_tool_photo_viewer(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
                "creator_tool": "Microsoft Windows Photo Viewer 10.0.26100.1882",
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertIn("creator_tool:Microsoft Windows Photo Viewer 10.0.26100.1882", tags)

    def test_no_creator_tool_tag_when_absent(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertFalse(any(t.startswith("creator_tool:") for t in tags))

    def test_editor_tags_from_editor_software(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
                "editor_software": ["Adobe Photoshop Express (Android)"],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertIn("editor:adobe", tags)
        self.assertIn("editor:adobe photoshop express", tags)

    def test_no_editor_tag_for_vrchat(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
                "creator_tool": "VRChat",
                "editor_software": ["VRChat"],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertFalse(any(t.startswith("editor:") for t in tags))

    def test_player_tags(self):
        meta = {
            1: {
                "author": {"id": "", "displayName": ""},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [
                    {"id": "usr_p1", "displayName": "Player1"},
                    {"id": "usr_p2", "displayName": "Player2"},
                ],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertIn("vrchat-user-id:usr_p1", tags)
        self.assertIn("vrchat-user-name:Player1", tags)
        self.assertIn("vrchat-user-id:usr_p2", tags)

    def test_date_tag_from_created(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
                "created": datetime(2025, 8, 30, 6, 45, 33, tzinfo=timezone(timedelta(hours=2))),
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertIn("vrchat-date:2025-08-30", tags)

    def test_no_date_tag_without_created(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "User"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertFalse(any(t.startswith("vrchat-date:") for t in tags))

    def test_empty_strings_excluded(self):
        meta = {
            1: {
                "author": {"id": "", "displayName": ""},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertEqual(tags, ["vrchat"])

    def test_instance_id_tag(self):
        meta = {
            1: {
                "author": {"id": "", "displayName": ""},
                "world": {"id": "wrld_abc", "instanceId": "wrld_abc:12345", "name": ""},
                "players": [],
            }
        }
        tags = build_file_id_to_tags(meta)[1]
        self.assertIn("vrchat-world-instanceId:wrld_abc:12345", tags)


class TestBuildEditorTags(unittest.TestCase):
    def test_adobe_express_brand_and_app(self):
        self.assertEqual(
            build_editor_tags(["Adobe Photoshop Express (Android)"]),
            ["editor:adobe", "editor:adobe photoshop express"],
        )

    def test_strips_version_numbers(self):
        self.assertEqual(build_editor_tags(["GIMP 2.10.34"]),
                         ["editor:gimp"])

    def test_photoshop_maps_to_adobe(self):
        tags = build_editor_tags(["Adobe Photoshop 25.0"])
        self.assertIn("editor:adobe", tags)
        self.assertIn("editor:adobe photoshop", tags)

    def test_vrchat_skipped(self):
        self.assertEqual(build_editor_tags(["VRChat"]), [])

    def test_unknown_editor_gets_app_only(self):
        # No known brand, but still tag the app name
        self.assertEqual(build_editor_tags(["SomeRandomTool"]),
                         ["editor:somerandomtool"])

    def test_dedup_across_inputs(self):
        tags = build_editor_tags([
            "Adobe Photoshop Express (Android)",
            "Adobe Photoshop Express (Android)",
        ])
        self.assertEqual(tags, ["editor:adobe", "editor:adobe photoshop express"])

    def test_empty_input(self):
        self.assertEqual(build_editor_tags([]), [])
        self.assertEqual(build_editor_tags(["", "   "]), [])


class TestBuildTagMappings(unittest.TestCase):
    def test_author_mappings(self):
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "TestUser"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            }
        }
        mappings = build_tag_mappings(meta)
        self.assertIn(("vrchat-user-id:usr_abc", "vrchat-user-name:TestUser"), mappings)
        self.assertIn(("vrchat-author-id:usr_abc", "vrchat-author-name:TestUser"), mappings)

    def test_world_mappings(self):
        meta = {
            1: {
                "author": {"id": "", "displayName": ""},
                "world": {"id": "wrld_xyz", "instanceId": "", "name": "MyWorld"},
                "players": [],
            }
        }
        mappings = build_tag_mappings(meta)
        self.assertIn(("vrchat-world-id:wrld_xyz", "vrchat-world-name:MyWorld"), mappings)

    def test_player_mappings(self):
        meta = {
            1: {
                "author": {"id": "", "displayName": ""},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [{"id": "usr_p1", "displayName": "Player1"}],
            }
        }
        mappings = build_tag_mappings(meta)
        self.assertIn(("vrchat-user-id:usr_p1", "vrchat-user-name:Player1"), mappings)

    def test_merge_with_existing_tags(self):
        """When existing tags are provided, new tags merge without duplicates."""
        meta = {
            1: {
                "author": {"id": "usr_abc", "displayName": "TestUser"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            }
        }
        existing = {1: ["vrchat", "custom-tag:manual"]}
        result = build_file_id_to_tags(meta, existing=existing)
        tags = result[1]
        # Original custom tag preserved
        self.assertIn("custom-tag:manual", tags)
        # New tags added
        self.assertIn("vrchat-author-id:usr_abc", tags)
        # No duplicates
        self.assertEqual(tags.count("vrchat"), 1)
        # Result is sorted
        self.assertEqual(tags, sorted(tags))

    def test_multiple_files(self):
        """Multiple file IDs each get their own tag set."""
        meta = {
            1: {
                "author": {"id": "usr_a", "displayName": "A"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            },
            2: {
                "author": {"id": "usr_b", "displayName": "B"},
                "world": {"id": "", "instanceId": "", "name": ""},
                "players": [],
            },
        }
        result = build_file_id_to_tags(meta)
        self.assertIn("vrchat-author-id:usr_a", result[1])
        self.assertIn("vrchat-author-id:usr_b", result[2])
        self.assertNotIn("vrchat-author-id:usr_b", result[1])


if __name__ == "__main__":
    unittest.main()
