import unittest

from core.meta_line_parser import parse_meta_line, MetaParseError


class TestParseMetaLine(unittest.TestCase):
    def test_screenshotmanager_basic(self):
        line = "screenshotmanager|0|author:usr_abc,TestUser|world:wrld_123,inst1,MyWorld"
        result = parse_meta_line(line)
        self.assertEqual(result["type"], "screenshotmanager")
        self.assertEqual(result["index"], 0)
        self.assertEqual(result["author"]["id"], "usr_abc")
        self.assertEqual(result["author"]["displayName"], "TestUser")
        self.assertEqual(result["world"]["id"], "wrld_123")
        self.assertEqual(result["world"]["name"], "MyWorld")

    def test_lfs_type(self):
        line = "lfs|5|author:usr_xyz,User2"
        result = parse_meta_line(line)
        self.assertEqual(result["type"], "lfs")
        self.assertEqual(result["index"], 5)

    def test_position(self):
        line = "screenshotmanager|0|pos:1.5,2.0,3.5"
        result = parse_meta_line(line)
        self.assertEqual(result["position"], {"x": 1.5, "y": 2.0, "z": 3.5})

    def test_rq(self):
        line = "screenshotmanager|0|rq:4"
        result = parse_meta_line(line)
        self.assertEqual(result["rq"], 4)

    def test_players(self):
        line = "screenshotmanager|0|players:usr_p1,1.0,2.0,3.0,Player1;usr_p2,4.0,5.0,6.0,Player2"
        result = parse_meta_line(line)
        self.assertEqual(len(result["players"]), 2)
        self.assertEqual(result["players"][0]["id"], "usr_p1")
        self.assertEqual(result["players"][0]["displayName"], "Player1")
        self.assertEqual(result["players"][1]["position"], {"x": 4.0, "y": 5.0, "z": 6.0})

    def test_unknown_type_raises(self):
        with self.assertRaises(MetaParseError):
            parse_meta_line("unknown|0")

    def test_too_few_parts_raises(self):
        with self.assertRaises(MetaParseError):
            parse_meta_line("screenshotmanager")

    def test_invalid_index_raises(self):
        with self.assertRaises(MetaParseError):
            parse_meta_line("screenshotmanager|abc")

    def test_invalid_author_warns(self):
        """Invalid author field is logged as warning, not raised (lenient parsing)."""
        result = parse_meta_line("screenshotmanager|0|author:no_comma")
        # Author stays at default since single-value author can't split
        self.assertEqual(result["author"]["id"], "")

    def test_invalid_pos_warns(self):
        """Invalid pos field is logged as warning, not raised (lenient parsing)."""
        result = parse_meta_line("screenshotmanager|0|pos:1.0,2.0")
        # Position stays at default since only 2 coords provided
        self.assertEqual(result["position"]["x"], 0.0)

    def test_unknown_keys_ignored(self):
        line = "screenshotmanager|0|future_field:some_value"
        result = parse_meta_line(line)
        self.assertEqual(result["type"], "screenshotmanager")

    def test_world_instanceid_format(self):
        line = "screenshotmanager|0|world:wrld_abc,12345,Test World"
        result = parse_meta_line(line)
        self.assertEqual(result["world"]["instanceId"], "wrld_abc:12345")


    def test_bare_wrld_segment(self):
        """Bare wrld_ segment (no 'world:' prefix) should be parsed as world."""
        line = "screenshotmanager|0|wrld_abc,12345,Test World"
        result = parse_meta_line(line)
        self.assertEqual(result["world"]["id"], "wrld_abc")
        self.assertEqual(result["world"]["name"], "Test World")
        self.assertEqual(result["world"]["instanceId"], "wrld_abc:12345")

    def test_malformed_player_entry_skipped(self):
        """Player with wrong number of fields is skipped, others kept."""
        line = "screenshotmanager|0|players:usr_p1,1.0,2.0,3.0,Player1;bad_entry;usr_p2,4.0,5.0,6.0,Player2"
        result = parse_meta_line(line)
        self.assertEqual(len(result["players"]), 2)
        self.assertEqual(result["players"][0]["id"], "usr_p1")
        self.assertEqual(result["players"][1]["id"], "usr_p2")

    def test_player_invalid_coords_skipped(self):
        """Player with non-numeric coords is skipped."""
        line = "screenshotmanager|0|players:usr_p1,x,y,z,Player1;usr_p2,1.0,2.0,3.0,Player2"
        result = parse_meta_line(line)
        self.assertEqual(len(result["players"]), 1)
        self.assertEqual(result["players"][0]["id"], "usr_p2")

    def test_invalid_rq_warns(self):
        """Non-integer rq stays at default."""
        result = parse_meta_line("screenshotmanager|0|rq:abc")
        self.assertEqual(result["rq"], 0)

    def test_world_too_few_parts_warns(self):
        """World with fewer than 3 comma-separated parts stays at default."""
        result = parse_meta_line("screenshotmanager|0|world:wrld_abc,12345")
        self.assertEqual(result["world"]["id"], "")


if __name__ == "__main__":
    unittest.main()
