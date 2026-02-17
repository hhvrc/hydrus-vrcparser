#!/usr/bin/env python3
"""Analyze broken metadata and parse failures - uses db_logic functions"""
import sqlite3
from pathlib import Path
from db_logic import db_find_files_without_metadata, db_find_unparseable_chunks, db_get_state_summary

# Check for broken_metadata directory
broken_dir = Path("broken_metadata")
print("\n" + "="*70)
print("BROKEN METADATA & PARSE FAILURE ANALYSIS")
print("="*70)

if broken_dir.exists():
    broken_files = list(broken_dir.glob("*.txt"))
    print(f"\n🔴 Found {len(broken_files)} broken metadata files")
    
    if broken_files:
        print("\nSample broken files:")
        for f in broken_files[:5]:
            try:
                content = f.read_text()[:200]
                preview = content.replace('\n', ' ')
                print(f"  - {f.name}: {preview}...")
            except Exception:
                print(f"  - {f.name}: (could not read)")
else:
    print("\n✓ No broken_metadata directory (all files processed successfully)")

# Analyze database
db_path = Path('vrchat.db')
if not db_path.exists():
    print("\n❌ Database doesn't exist yet")
    exit(1)

conn = sqlite3.connect(str(db_path))
conn.row_factory = sqlite3.Row

print("\n" + "="*70)
print("DATABASE ANALYSIS")
print("="*70)

# State overview
state = db_get_state_summary(conn)
print(f"\n📊 Total files: {state.get('total_files', 0)}")
print(f"   iTXt chunks: {state.get('total_itxt_chunks', 0)}")
print(f"   With metadata: {state.get('files_with_metadata', 0)}")

# Processed files with no VRC metadata
no_metadata = db_find_files_without_metadata(conn)
if no_metadata:
    print(f"\n   Processed, no VRC metadata: {len(no_metadata)} files")
    print("   (Scanned for iTXt but contained no VRC embedded data)")
else:
    print("\n   All processed files contain iTXt chunks")

# Unparseable chunks
unparseable = db_find_unparseable_chunks(conn)
if unparseable:
    print(f"\n⚠️  UNPARSEABLE ITXT CHUNKS ({len(unparseable)} keyword/type combos):")
    for row in unparseable:
        print(f"     {row['keyword']:25} (type={row['content_type'] or 'NULL':8}): {row['cnt']:4} chunks")
else:
    print("\n✓ All iTXt chunks are parseable")

conn.close()
print("\n" + "="*70 + "\n")

