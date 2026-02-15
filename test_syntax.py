#!/usr/bin/env python3
import py_compile
import sys

files = [
    'db_logic.py',
    'config_io.py',
    'core/png_itxt.py',
    'core/meta_line_parser.py',
    'hydrus-vrcparser.py',
    'cli.py',
    'hydrus_io.py',
]

errors = []
for f in files:
    try:
        py_compile.compile(f, doraise=True)
        print(f"✓ {f}")
    except py_compile.PyCompileError as e:
        print(f"✗ {f}: {e}")
        errors.append(f)

if errors:
    print(f"\n{len(errors)} file(s) with syntax errors")
    sys.exit(1)
else:
    print(f"\nAll {len(files)} files have valid syntax!")
    sys.exit(0)
