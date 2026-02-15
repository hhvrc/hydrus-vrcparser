#!/usr/bin/env python3
"""
Comprehensive test runner: syntax check, db state, parse, verify, and analyze.
"""
import subprocess
import sys

def run_cmd(cmd, desc):
    """Run command and report status"""
    print(f"\n{'='*70}")
    print(f"▶️  {desc}")
    print(f"{'='*70}")
    try:
        result = subprocess.run(cmd, capture_output=False, text=True)
        return result.returncode == 0
    except Exception as e:
        print(f"❌ Error: {e}")
        return False

# 1. Syntax check
print("\n" + "="*70)
print("STEP 1: SYNTAX CHECK")
print("="*70)
files = ['db_logic.py', 'config_io.py', 'core/png_itxt.py', 'core/meta_line_parser.py', 'hydrus-vrcparser.py']
all_ok = True
for f in files:
    try:
        import py_compile
        py_compile.compile(f, doraise=True)
        print(f"✓ {f}")
    except Exception as e:
        print(f"✗ {f}: {e}")
        all_ok = False

if not all_ok:
    print("\n❌ Fix syntax errors first")
    sys.exit(1)

# 2. DB state before
run_cmd([sys.executable, 'check_db.py'], "Database state BEFORE")

# 3. Run parser
run_cmd([sys.executable, 'hydrus-vrcparser.py'], "Running parser")

# 4. DB state after
run_cmd([sys.executable, 'check_db.py'], "Database state AFTER")

# 5. Failure analysis
run_cmd([sys.executable, 'analyze_failures.py'], "Analyzing failures & broken metadata")

# 6. Tests
run_cmd([sys.executable, '-m', 'unittest', 'discover', '-s', 'tests', '-v'], "Unit tests")

print("\n" + "="*70)
print("✓ TEST RUN COMPLETE")
print("="*70)

