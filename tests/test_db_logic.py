import unittest
import tempfile
import os
from pathlib import Path

from db_logic import (
    hydrus_path_for_hash,
    init_db,
    db_replace_itxt_chunks,
    db_load_all_parsed_meta,
    db_get_or_create_data_dir_id,
    db_mark_processed_success,
    db_mark_processed_failure,
    db_mark_data_parsed,
    db_existing_processed_file_ids,
    db_file_has_itxt_chunks,
    db_get_hashes_for_ids,
    db_upsert_push_info,
    db_get_push_info,
    db_bulk_replace_tag_mappings,
    db_get_state_summary,
    db_get_migration_status,
    db_find_inconsistent_versions,
    db_reset_inconsistent_versions,
)
from core.constants import DATA_PARSER_VERSION


class TestHydrusPathForHash(unittest.TestCase):
    def test_basic_path(self):
        p = hydrus_path_for_hash(Path("/data"), "abcdef1234567890", "png")
        self.assertEqual(p, Path("/data/fab/abcdef1234567890.png"))

    def test_strips_leading_dot_from_ext(self):
        p = hydrus_path_for_hash(Path("/data"), "abcdef1234567890", ".PNG")
        self.assertEqual(p, Path("/data/fab/abcdef1234567890.png"))

    def test_default_ext(self):
        p = hydrus_path_for_hash(Path("/data"), "ff0011aabbcc")
        self.assertEqual(p, Path("/data/fff/ff0011aabbcc.png"))


class TestInitDb(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.db_path = Path(self.tmp) / "test.db"

    def tearDown(self):
        if self.db_path.exists():
            os.remove(self.db_path)
        os.rmdir(self.tmp)

    def test_creates_all_tables(self):
        conn = init_db(self.db_path)
        tables = {
            row[0]
            for row in conn.execute(
                "SELECT name FROM sqlite_master WHERE type='table'"
            ).fetchall()
        }
        for expected in ("files", "itxt_chunks", "hydrus_meta", "tag_mappings",
                         "hash_tags", "pushes", "data_dirs", "schema_migrations"):
            self.assertIn(expected, tables)
        conn.close()

    def test_idempotent(self):
        """init_db can be called twice without error."""
        conn1 = init_db(self.db_path)
        conn1.close()
        conn2 = init_db(self.db_path)
        conn2.close()

    def test_migrations_applied(self):
        conn = init_db(self.db_path)
        status = db_get_migration_status(conn)
        self.assertIn("004_reclassify_line_content_type", status)
        self.assertIn("005_drop_legacy_file_columns", status)
        conn.close()


class TestDbWithFixtures(unittest.TestCase):
    """Base class that sets up an in-memory DB with test data."""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.db_path = Path(self.tmp) / "test.db"
        self.conn = init_db(self.db_path)

        # Create a data_dir
        self.data_dir = Path(self.tmp) / "client_files"
        self.data_dir.mkdir()
        self.data_dir_id = db_get_or_create_data_dir_id(self.conn, self.data_dir)

        # Create a dummy file on disk so ensure_file_record can stat it
        self.test_hash = "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
        file_dir = self.data_dir / f"f{self.test_hash[:2]}"
        file_dir.mkdir()
        (file_dir / f"{self.test_hash}.png").write_bytes(b"\x00" * 100)

        # Insert a file record directly
        with self.conn:
            self.conn.execute(
                "INSERT INTO files(file_id, hash, file_ext, data_dir_id, created_at, size) "
                "VALUES(?, ?, ?, ?, ?, ?)",
                (1, self.test_hash, "png", self.data_dir_id, "2024-01-01T00:00:00Z", 100),
            )

    def tearDown(self):
        self.conn.close()
        import shutil
        shutil.rmtree(self.tmp)


class TestDataDirId(TestDbWithFixtures):
    def test_get_existing(self):
        """Same path returns same ID."""
        id2 = db_get_or_create_data_dir_id(self.conn, self.data_dir)
        self.assertEqual(id2, self.data_dir_id)

    def test_create_new(self):
        """Different path gets a new ID."""
        id2 = db_get_or_create_data_dir_id(self.conn, Path("/other/path"))
        self.assertNotEqual(id2, self.data_dir_id)


class TestItxtChunkOperations(TestDbWithFixtures):
    def test_replace_and_check(self):
        descriptors = [
            (0, "Description", 0, 0, "", "", '{"author": {}}', "json"),
        ]
        db_replace_itxt_chunks(self.conn, 1, descriptors)
        self.assertTrue(db_file_has_itxt_chunks(self.conn, 1))

    def test_no_chunks(self):
        self.assertFalse(db_file_has_itxt_chunks(self.conn, 1))

    def test_replace_clears_old(self):
        db_replace_itxt_chunks(self.conn, 1, [
            (0, "Description", 0, 0, "", "", "old", "text"),
        ])
        db_replace_itxt_chunks(self.conn, 1, [
            (0, "Description", 0, 0, "", "", "new", "text"),
        ])
        rows = self.conn.execute(
            "SELECT text FROM itxt_chunks WHERE file_id = 1"
        ).fetchall()
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["text"], "new")


class TestProcessedVersionTracking(TestDbWithFixtures):
    def test_mark_success_sets_file_version(self):
        db_mark_processed_success(self.conn, 1)
        ids = db_existing_processed_file_ids(self.conn)
        self.assertIn(1, ids)

    def test_unprocessed_not_in_set(self):
        ids = db_existing_processed_file_ids(self.conn)
        self.assertNotIn(1, ids)

    def test_mark_failure_does_not_set_version(self):
        db_mark_processed_failure(self.conn, 1)
        ids = db_existing_processed_file_ids(self.conn)
        self.assertNotIn(1, ids)

    def test_mark_data_parsed(self):
        db_mark_data_parsed(self.conn, [1])
        row = self.conn.execute(
            "SELECT data_parser_version FROM files WHERE file_id = 1"
        ).fetchone()
        self.assertEqual(row["data_parser_version"], DATA_PARSER_VERSION)


class TestLoadAllParsedMeta(TestDbWithFixtures):
    def test_json_priority_over_line(self):
        """JSON chunk should take priority over line chunk for same file_id."""
        db_replace_itxt_chunks(self.conn, 1, [
            (0, "Description", 0, 0, "", "",
             '{"author": {"id": "usr_json", "displayName": "JsonUser"}, '
             '"world": {"id": "wrld_123", "name": "W"}}',
             "json"),
            (1, "Description", 0, 0, "", "",
             "screenshotmanager|0|author:usr_line,LineUser",
             "line"),
        ])
        result = db_load_all_parsed_meta(self.conn)
        self.assertIn(1, result)
        self.assertEqual(result[1]["author"]["id"], "usr_json")

    def test_xml_priority_over_line(self):
        """XML chunk should take priority over line chunk."""
        xmp = (
            '<x:xmpmeta xmlns:x="adobe:ns:meta/">'
            '<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">'
            '<rdf:Description>'
            '<xmp:CreatorTool xmlns:xmp="http://ns.adobe.com/xap/1.0/">VRChat</xmp:CreatorTool>'
            '<xmp:Author xmlns:xmp="http://ns.adobe.com/xap/1.0/">TestAuthor</xmp:Author>'
            '<vrc:WorldID xmlns:vrc="http://ns.vrchat.com/vrc/1.0/">'
            'wrld_68bebba1-e5ed-40ff-84c1-f17544a2ffbe</vrc:WorldID>'
            '</rdf:Description>'
            '</rdf:RDF>'
            '</x:xmpmeta>'
        )
        db_replace_itxt_chunks(self.conn, 1, [
            (0, "XML:com.adobe.xmp", 0, 0, "", "", xmp, "xml"),
            (1, "Description", 0, 0, "", "",
             "screenshotmanager|0|author:usr_line,LineUser",
             "line"),
        ])
        result = db_load_all_parsed_meta(self.conn)
        self.assertIn(1, result)
        # XML should win over line
        self.assertEqual(result[1]["author"]["displayName"], "TestAuthor")

    def test_line_format_parsed(self):
        db_replace_itxt_chunks(self.conn, 1, [
            (0, "Description", 0, 0, "", "",
             "screenshotmanager|0|author:usr_abc,TestUser|world:wrld_123,inst1,MyWorld",
             "line"),
        ])
        result = db_load_all_parsed_meta(self.conn)
        self.assertIn(1, result)
        self.assertEqual(result[1]["author"]["id"], "usr_abc")
        self.assertEqual(result[1]["world"]["name"], "MyWorld")

    def test_broken_json_skipped(self):
        db_replace_itxt_chunks(self.conn, 1, [
            (0, "Description", 0, 0, "", "", "{invalid json", "json"),
        ])
        result = db_load_all_parsed_meta(self.conn)
        self.assertNotIn(1, result)

    def test_empty_db_returns_empty(self):
        result = db_load_all_parsed_meta(self.conn)
        self.assertEqual(result, {})


class TestPushInfo(TestDbWithFixtures):
    def test_upsert_and_get(self):
        db_upsert_push_info(self.conn, 1, "hash123")
        row = db_get_push_info(self.conn, 1)
        self.assertIsNotNone(row)
        self.assertEqual(row["tag_hash"], "hash123")

    def test_upsert_updates_hash(self):
        db_upsert_push_info(self.conn, 1, "hash_old")
        db_upsert_push_info(self.conn, 1, "hash_new")
        row = db_get_push_info(self.conn, 1)
        self.assertEqual(row["tag_hash"], "hash_new")

    def test_get_nonexistent(self):
        self.assertIsNone(db_get_push_info(self.conn, 999))


class TestBulkReplaceTagMappings(TestDbWithFixtures):
    def test_replaces_all(self):
        db_bulk_replace_tag_mappings(self.conn, [("p1", "c1"), ("p2", "c2")])
        count = self.conn.execute("SELECT COUNT(*) FROM tag_mappings").fetchone()[0]
        self.assertEqual(count, 2)

    def test_replaces_previous(self):
        db_bulk_replace_tag_mappings(self.conn, [("p1", "c1")])
        db_bulk_replace_tag_mappings(self.conn, [("p2", "c2"), ("p3", "c3")])
        count = self.conn.execute("SELECT COUNT(*) FROM tag_mappings").fetchone()[0]
        self.assertEqual(count, 2)


class TestGetHashesForIds(TestDbWithFixtures):
    def test_returns_mapping(self):
        result = db_get_hashes_for_ids(self.conn, [1])
        self.assertEqual(result[1], self.test_hash)

    def test_empty_input(self):
        self.assertEqual(db_get_hashes_for_ids(self.conn, []), {})

    def test_missing_id(self):
        result = db_get_hashes_for_ids(self.conn, [999])
        self.assertNotIn(999, result)


class TestStateSummary(TestDbWithFixtures):
    def test_returns_counts(self):
        summary = db_get_state_summary(self.conn)
        self.assertEqual(summary["total_files"], 1)
        self.assertIn("total_itxt_chunks", summary)
        self.assertNotIn("error", summary)


if __name__ == "__main__":
    unittest.main()
