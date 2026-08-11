"""Structurally compare the EF-generated schema against the live vrchat.db.

Named vs unnamed constraints are expected to differ (EF names everything);
what must match is tables, columns, types, nullability, defaults, primary keys,
foreign keys and indexes.
"""
import sqlite3
import sys
from pathlib import Path

SCRATCH = Path(__file__).parent
LIVE_DB = Path(sys.argv[1])
EF_SQL = SCRATCH / "baseline.sql"

# EF's own bookkeeping table, and SQLite internals, are not part of the comparison.
IGNORE_TABLES = {"__EFMigrationsHistory", "sqlite_sequence"}


def build_ef_db() -> sqlite3.Connection:
    conn = sqlite3.connect(":memory:")
    sql = EF_SQL.read_text(encoding="utf-8-sig")
    # The script wraps DDL in an explicit transaction; executescript manages its own.
    sql = sql.replace("BEGIN TRANSACTION;", "").replace("COMMIT;", "")
    conn.executescript(sql)
    return conn


def tables(conn):
    rows = conn.execute(
        "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name"
    ).fetchall()
    return {r[0] for r in rows} - IGNORE_TABLES


def columns(conn, table):
    # (name, type, notnull, default, pk_position)
    return {
        r[1]: (r[2].upper(), r[3], normalize_default(r[4]), r[5])
        for r in conn.execute(f'PRAGMA table_info("{table}")')
    }


def normalize_default(d):
    if d is None:
        return None
    return str(d).strip().strip("'\"")


def foreign_keys(conn, table):
    # (child column, parent table, parent column) -- constraint names are not compared.
    return {
        (r[3], r[2], r[4])
        for r in conn.execute(f'PRAGMA foreign_key_list("{table}")')
    }


def indexes(conn, table):
    """Named indexes and their columns. Auto-indexes (from UNIQUE/PK) are
    compared separately by uniqueness signature, since EF names them and the
    hand-written schema does not."""
    named, auto_unique = {}, set()
    for r in conn.execute(f'PRAGMA index_list("{table}")'):
        name, unique, origin = r[1], bool(r[2]), r[3]
        cols = tuple(x[2] for x in conn.execute(f'PRAGMA index_info("{name}")'))
        if origin == "c":  # CREATE INDEX
            named[name] = (unique, cols)
        elif unique:  # 'u' (UNIQUE constraint) or 'pk'
            auto_unique.add(cols)
    return named, auto_unique


def main():
    live = sqlite3.connect(f"file:{LIVE_DB}?mode=ro", uri=True)
    ef = build_ef_db()

    problems = []
    expected = []

    lt, et = tables(live), tables(ef)
    if lt != et:
        for t in sorted(lt - et):
            problems.append(f"TABLE missing from EF model: {t}")
        for t in sorted(et - lt):
            problems.append(f"TABLE only in EF model: {t}")

    for table in sorted(lt & et):
        lc, ec = columns(live, table), columns(ef, table)
        for col in sorted(set(lc) | set(ec)):
            if col not in lc:
                problems.append(f"{table}.{col}: only in EF model")
            elif col not in ec:
                problems.append(f"{table}.{col}: missing from EF model")
            elif lc[col] != ec[col]:
                ltype, lnn, ldef, lpk = lc[col]
                etype, enn, edef, epk = ec[col]
                # SQLite quirk: a single-column "INTEGER PRIMARY KEY" is a rowid
                # alias and is reported notnull=0 even though NULL is rejected
                # (a rowid gets auto-assigned). EF spells NOT NULL out, giving
                # notnull=1. Both reject NULL; the declarations are equivalent.
                rowid_alias = (
                    ltype == etype == "INTEGER"
                    and lpk == epk == 1
                    and ldef == edef
                    and (lnn, enn) == (0, 1)
                )
                if rowid_alias:
                    expected.append(f"{table}.{col}: INTEGER PRIMARY KEY rowid alias (notnull 0 vs 1)")
                else:
                    problems.append(
                        f"{table}.{col}: live={lc[col]} ef={ec[col]}  "
                        f"(type, notnull, default, pk_pos)"
                    )

        lf, ef_fk = foreign_keys(live, table), foreign_keys(ef, table)
        if lf != ef_fk:
            problems.append(f"{table}: FK mismatch live={sorted(lf)} ef={sorted(ef_fk)}")

        (ln, lu), (en, eu) = indexes(live, table), indexes(ef, table)
        if ln != en:
            problems.append(f"{table}: named index mismatch live={ln} ef={en}")
        if lu != eu:
            problems.append(f"{table}: unique-constraint columns live={sorted(lu)} ef={sorted(eu)}")

    print(f"live tables: {len(lt)}  ef tables: {len(et)}")
    if expected:
        print(f"\n{len(expected)} EXPECTED, SEMANTICALLY-EQUIVALENT DIFFERENCE(S):")
        for e in expected:
            print(f"  . {e}")
    if problems:
        print(f"\n{len(problems)} DIFFERENCE(S):")
        for p in problems:
            print(f"  - {p}")
        return 1

    print("\nSCHEMA MATCH: tables, columns, types, defaults, PKs, FKs and indexes all agree.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
