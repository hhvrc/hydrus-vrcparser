import unittest
from datetime import datetime, timezone

from db_logic import _normalize_meta


class TestNormalizeMeta(unittest.TestCase):
    def test_basic_normalization(self):
        meta = {
            "type": "xmp",
            "author": {"id": "usr_abc", "displayName": "User"},
            "world": {"id": "wrld_xyz", "name": "World"},
        }
        result = _normalize_meta(meta, "raw")
        self.assertEqual(result["type"], "xmp")
        self.assertEqual(result["author"]["id"], "usr_abc")
        self.assertEqual(result["author"]["displayName"], "User")
        self.assertEqual(result["world"]["id"], "wrld_xyz")
        self.assertEqual(result["world"]["name"], "World")
        self.assertEqual(result["raw_text"], "raw")

    def test_missing_fields_default(self):
        result = _normalize_meta({}, "raw")
        self.assertEqual(result["author"]["id"], "")
        self.assertEqual(result["author"]["displayName"], "")
        self.assertEqual(result["world"]["id"], "")
        self.assertEqual(result["world"]["instanceId"], "")
        self.assertEqual(result["position"], {"x": 0.0, "y": 0.0, "z": 0.0})
        self.assertEqual(result["rq"], 0)
        self.assertEqual(result["players"], [])

    def test_author_name_fallback(self):
        meta = {"author": {"id": "usr_abc", "name": "FallbackName"}}
        result = _normalize_meta(meta, "raw")
        self.assertEqual(result["author"]["displayName"], "FallbackName")

    def test_position(self):
        meta = {"position": {"x": "1.5", "y": 2.0, "z": "3.5"}}
        result = _normalize_meta(meta, "raw")
        self.assertEqual(result["position"], {"x": 1.5, "y": 2.0, "z": 3.5})

    def test_invalid_position_ignored(self):
        meta = {"position": {"x": "not_a_number", "y": 1.0, "z": 2.0}}
        result = _normalize_meta(meta, "raw")
        self.assertEqual(result["position"]["x"], 0.0)
        self.assertEqual(result["position"]["y"], 1.0)

    def test_created_passthrough(self):
        dt = datetime(2025, 1, 1, tzinfo=timezone.utc)
        meta = {"created": dt}
        result = _normalize_meta(meta, "raw")
        self.assertEqual(result["created"], dt)

    def test_players_passthrough(self):
        players = [{"id": "usr_p1", "displayName": "P1"}]
        meta = {"players": players}
        result = _normalize_meta(meta, "raw")
        self.assertEqual(result["players"], players)

    def test_none_author(self):
        result = _normalize_meta({"author": None}, "raw")
        self.assertEqual(result["author"]["id"], "")
        self.assertEqual(result["author"]["displayName"], "")


if __name__ == "__main__":
    unittest.main()
