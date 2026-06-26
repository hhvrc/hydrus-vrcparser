import unittest

from core.user_correlation import correlate


def _f(ids, names):
    return (set(ids), set(names))


class TestCorrelate(unittest.TestCase):
    def test_perfect_overlap_is_already_paired(self):
        # id A and name Alice appear on exactly the same two files -> paired.
        files = [
            _f(["A"], ["Alice"]),
            _f(["A"], ["Alice"]),
        ]
        result = correlate(files)
        self.assertIn(("A", "Alice"), result.paired_pairs)
        self.assertEqual(result.suggestions, [])

    def test_partial_overlap_is_suggested(self):
        # A and Alice clearly belong together: they share 3 files, and the name
        # dropped off one file (id-only) while staying the dominant partner.
        files = [
            _f(["A"], ["Alice"]),
            _f(["A"], ["Alice"]),
            _f(["A"], ["Alice"]),
            _f(["A"], []),          # name failed to parse here
        ]
        result = correlate(files, min_overlap=2, min_jaccard=0.5)
        self.assertEqual(result.paired_pairs, [])  # not identical file sets
        self.assertEqual(len(result.suggestions), 1)
        s = result.suggestions[0]
        self.assertEqual((s.user_id, s.name), ("A", "Alice"))
        self.assertEqual(s.overlap, 3)
        self.assertEqual(s.id_files, 4)
        self.assertEqual(s.name_files, 3)

    def test_below_overlap_threshold_rejected(self):
        files = [
            _f(["A"], ["Alice"]),
            _f(["A"], []),
        ]
        result = correlate(files, min_overlap=2)
        self.assertEqual(result.suggestions, [])
        self.assertEqual(len(result.rejected), 1)

    def test_ambiguous_runner_up_rejected(self):
        # A co-occurs equally with Alice and Beth -> not a clear winner.
        files = [
            _f(["A"], ["Alice", "Beth"]),
            _f(["A"], ["Alice", "Beth"]),
            _f(["A"], ["Alice", "Beth"]),
        ]
        result = correlate(files, min_overlap=2, min_jaccard=0.3, max_runner_up_ratio=0.6)
        self.assertEqual(result.suggestions, [])

    def test_mutual_best_match_required(self):
        # B shares more files with Alice than A does, so A should not steal Alice.
        files = [
            _f(["A"], ["Alice"]),
            _f(["B"], ["Alice"]),
            _f(["B"], ["Alice"]),
            _f(["B"], ["Alice"]),
            _f(["B"], []),
        ]
        result = correlate(files, min_overlap=1, min_jaccard=0.1, max_runner_up_ratio=1.0)
        paired_or_suggested_for_alice = [
            s for s in result.suggestions if s.name == "Alice"
        ]
        # Alice's only suggested partner (if any) must be B, never A.
        for s in paired_or_suggested_for_alice:
            self.assertEqual(s.user_id, "B")

    def test_orphans_detected(self):
        files = [
            _f(["A"], []),        # id never seen with any name
            _f([], ["Lonely"]),   # name never seen with any id
        ]
        result = correlate(files)
        self.assertIn(("A", 1), result.orphan_ids)
        self.assertIn(("Lonely", 1), result.orphan_names)
        self.assertEqual(result.suggestions, [])

    def test_inseparable_ids_not_guessed(self):
        # A and B appear on the exact same files, so co-occurrence cannot tell
        # which is Alice and which is Bob. Strict mode must refuse to guess.
        files = [
            _f(["A", "B"], ["Alice", "Bob"]),
            _f(["A", "B"], ["Alice", "Bob"]),
            _f(["A", "B"], ["Alice"]),
        ]
        result = correlate(files, min_overlap=2, min_jaccard=0.5)
        self.assertEqual(result.paired_pairs, [])
        self.assertEqual(result.suggestions, [])

    def test_multi_user_elimination(self):
        # B appears once alone with Bob, which distinguishes it from A. Then
        # A<->Alice is an exclusive perfect overlap and B<->Bob is inferred.
        files = [
            _f(["A", "B"], ["Alice", "Bob"]),
            _f(["A", "B"], ["Alice", "Bob"]),
            _f(["A", "B"], ["Alice"]),   # Bob's name missing here
            _f(["B"], ["Bob"]),          # B seen alone with Bob
        ]
        result = correlate(files, min_overlap=2, min_jaccard=0.5)
        self.assertIn(("A", "Alice"), result.paired_pairs)
        suggested = {(s.user_id, s.name) for s in result.suggestions}
        self.assertIn(("B", "Bob"), suggested)


if __name__ == "__main__":
    unittest.main()
