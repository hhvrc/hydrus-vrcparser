# Parity harness

Verifies that the C# port produces byte-identical results to the legacy Python
implementation, judged against the real cached data in `vrchat.db` rather than
synthetic fixtures. Unit tests cover the cases we thought of; this covers the
ones we did not.

Always run against a **copy** of the database.

```bash
cp vrchat.db /tmp/vrchat_copy.db
```

## Schema comparison

Checks the EF Core model against the live schema (tables, columns, types,
defaults, primary keys, foreign keys, indexes).

```bash
python tools/parity/compare_schema.py /tmp/vrchat_copy.db
```

Five differences are expected and reported separately: SQLite treats a
single-column `INTEGER PRIMARY KEY` as a rowid alias and reports `notnull=0`
even though it rejects NULL, while EF writes `NOT NULL` explicitly.

## Per-chunk parser comparison

Parses every cached iTXt chunk with both implementations and diffs the
normalized fields. Deliberately per-chunk rather than per-file, so a mismatch
points at a specific parser instead of at the priority contest.

```bash
# Reference dump from the legacy Python
cd legacy-python && PYTHONPATH=. python ../tools/parity/dump_python_parse.py \
    ../vrchat.db /tmp/py_chunks.jsonl && cd ..

# Same dump from the C# port
dotnet run --project src/HydrusTagger.Cli -- \
    dump-chunk-parse /tmp/vrchat_copy.db /tmp/cs_chunks.jsonl

python tools/parity/compare_chunks.py /tmp/py_chunks.jsonl /tmp/cs_chunks.jsonl
```

Compares `effective_type`, `author_id`, `author_name`, `world_id`,
`world_name`, `instance_id`, `creator_tool`, `editor_software`, `created`,
`players` and `error` across all 13,402 chunks. Anything other than
"identical on every field of every chunk" is a regression.

Note that ~76% of chunks legitimately end in `MetaParseError`: they are Adobe
XMP packets from non-VRChat images that carry no `vrc:` namespace. Both
implementations must agree on those failures too, which is why `error` is one
of the compared fields.
