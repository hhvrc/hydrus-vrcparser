import unittest

from core.utils import sanitize_itxt_text, chunked


class TestSanitizeItxtText(unittest.TestCase):
    def test_strips_nul(self):
        self.assertEqual(sanitize_itxt_text("\x00\x00hello"), "hello")

    def test_strips_bom(self):
        self.assertEqual(sanitize_itxt_text("\ufeffhello"), "hello")

    def test_strips_whitespace(self):
        self.assertEqual(sanitize_itxt_text("  hello  "), "hello")

    def test_none_returns_empty(self):
        self.assertEqual(sanitize_itxt_text(None), "")

    def test_empty_returns_empty(self):
        self.assertEqual(sanitize_itxt_text(""), "")

    def test_combined(self):
        self.assertEqual(sanitize_itxt_text("\x00\ufeff  text  "), "text")


class TestChunked(unittest.TestCase):
    def test_even_split(self):
        result = list(chunked([1, 2, 3, 4], 2))
        self.assertEqual(result, [[1, 2], [3, 4]])

    def test_uneven_split(self):
        result = list(chunked([1, 2, 3, 4, 5], 2))
        self.assertEqual(result, [[1, 2], [3, 4], [5]])

    def test_empty(self):
        result = list(chunked([], 5))
        self.assertEqual(result, [])

    def test_single_chunk(self):
        result = list(chunked([1, 2, 3], 10))
        self.assertEqual(result, [[1, 2, 3]])


if __name__ == "__main__":
    unittest.main()
