#!/usr/bin/env python3
"""Quick database diagnostic - uses db_logic functions"""
import sqlite3
from pathlib import Path
from db_logic import db_print_diagnostic_report

db_path = Path('vrchat.db')
if not db_path.exists():
    print("❌ Database doesn't exist yet")
    exit(1)

conn = sqlite3.connect(str(db_path))
conn.row_factory = sqlite3.Row
db_print_diagnostic_report(conn)
conn.close()
