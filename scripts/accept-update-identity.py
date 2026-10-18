#!/usr/bin/env python3
"""Run the native updater on disposable copies of real signed bundles.

Usage: --old /path/0.3.7/Voicemore.app --archive /path/0.3.8.tar.gz
       --development /path/development/Voicemore.app --out /private/evidence
This does not launch an app, touch TCC, use private update keys or publish.
"""
import argparse
from pathlib import Path
import subprocess

p = argparse.ArgumentParser(description=__doc__)
for name in ('old', 'archive', 'development', 'out'):
    p.add_argument('--' + name, type=Path, required=True)
a = p.parse_args()
root = Path(__file__).resolve().parent.parent
subprocess.run(['go', 'run', '-tags', 'updateacceptance', './cmd/update-acceptance',
                '--old', a.old.resolve(), '--archive', a.archive.resolve(),
                '--development', a.development.resolve(), '--out', a.out.resolve()], cwd=root, check=True)
