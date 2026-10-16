#!/usr/bin/env python3
"""Mounted-artifact acceptance: layout, localization and native label resolution."""
import json
from pathlib import Path
import subprocess
import sys
from ds_store import DSStore

mount, evidence = map(Path, sys.argv[1:3])
root = Path(__file__).resolve().parent.parent
hint = 'Drag Voicemore to Applications to install.localized'
assert (mount / '.background.tiff').is_file(), 'Missing Retina artwork'
assert (mount / hint / 'Icon\r').exists(), 'Missing transparent native hint icon'
with DSStore.open(str(mount / '.DS_Store'), 'r') as store:
    options = store['.']['icvp']
    assert options['backgroundType'] == 2
    assert options['iconSize'] == 160 and options['textSize'] == 14
    assert options['gridSpacing'] <= 100, 'Finder rejects settings above 100'
    assert options['backgroundImageAlias'], 'Missing mounted-volume artwork alias'
    positions = {name: store[name]['Iloc'] for name in ('Voicemore.app', 'Applications', hint)}
    assert positions == {'Voicemore.app': (228, 214), 'Applications': (539, 214), hint: (384, 282)}
    window = store['.']['bwsp']
    assert all(not window[k] for k in ('ShowStatusBar','ShowToolbar','ShowSidebar','ShowPathbar'))
expected = {
    'en': 'Drag Voicemore to Applications to install',
    'zh-Hans': '将 Voicemore 拖入应用程序即可安装',
    'zh-Hant': '將 Voicemore 拖入應用程式即可安裝',
    'zh-CN': '将 Voicemore 拖入应用程序即可安装',
    'zh-TW': '將 Voicemore 拖入應用程式即可安裝',
    'fr': 'Drag Voicemore to Applications to install',
}
results = {}
for language, text in expected.items():
    value = subprocess.check_output([
        'swift', str(root / 'scripts/dmg-localization.swift'), '--display-name',
        str(mount / hint), '-AppleLanguages', f'({language})',
    ], text=True).strip()
    assert value == text, f'{language}: {value!r} != {text!r}'
    results[language] = value
report = {'status':'passed','scope':'mounted-artifact and native FileManager localization; not a Finder screenshot for each language',
          'positions':positions, 'languages':results, 'window':window}
(evidence / 'layout-localization.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
print('PASS: mounted layout, Retina background, native localization in six language configurations.')
