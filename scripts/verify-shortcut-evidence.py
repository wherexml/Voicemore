#!/usr/bin/env python3
"""Validate the recorded native fixture workflow; nonzero on missing evidence."""
import json
from pathlib import Path
import sys

rows = json.loads(Path(sys.argv[1]).read_text())
checks = []
for key in ('A', 'Shift+A', 'Enter', 'Space'):
    checks.append(dict(case='native reject ' + key, passed=any(
        r.get('captured') == key and r.get('outcome') == 'rejected'
        and r.get('before') == r.get('saved') for r in rows)))
checks.append(dict(case='native save Cmd+Shift+K', passed=any(
    r.get('captured') == 'Cmd+Shift+K' and r.get('outcome') == 'saved'
    and r.get('saved') == 'Cmd+Shift+K' for r in rows)))
checks.append(dict(case='Escape and Tab cancellation preserve saved key', passed=sum(
    r.get('outcome') == 'cancelled' and r.get('before') == 'Cmd+Shift+K'
    and r.get('saved') == 'Cmd+Shift+K' for r in rows) >= 2))
for name in ('safe accelerator registers with macOS', 'native recorded key survives reopening'):
    checks.append(dict(case=name, passed=any(r.get('case') == name and r.get('passed') is True for r in rows)))
json.dump(dict(passed=all(r['passed'] for r in checks), checks=checks), sys.stdout, indent=2)
print()
if not all(r['passed'] for r in checks):
    sys.exit(1)
