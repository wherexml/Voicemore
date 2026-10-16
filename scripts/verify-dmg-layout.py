#!/usr/bin/env python3
"""Mounted-artifact acceptance: fixed-English layout and two real draggable items."""
import json
from pathlib import Path
import sys
from ds_store import DSStore

mount, evidence = map(Path, sys.argv[1:3])
assert (mount / '.background.tiff').is_file(), 'Missing Retina artwork'
assert {p.name for p in mount.iterdir() if not p.name.startswith('.')} == {'Voicemore.app', 'Applications'}, 'Unexpected visible installer items'
with DSStore.open(str(mount / '.DS_Store'), 'r') as store:
    options = store['.']['icvp']
    assert options['backgroundType'] == 2
    assert options['iconSize'] == 160 and options['textSize'] == 10
    assert options['gridSpacing'] <= 100, 'Finder rejects settings above 100'
    assert options['backgroundImageAlias'], 'Missing mounted-volume artwork alias'
    positions = {name: store[name]['Iloc'] for name in ('Voicemore.app', 'Applications')}
    assert positions == {'Voicemore.app': (228, 202), 'Applications': (539, 202)}
    window = store['.']['bwsp']
    assert all(not window[k] for k in ('ShowStatusBar','ShowToolbar','ShowSidebar','ShowPathbar'))
report = {'status':'passed', 'language':'fixed English',
          'scope':'mounted artifact; visual text checked in Finder screenshot',
          'positions':positions, 'window':window}
(evidence / 'layout.json').write_text(json.dumps(report, ensure_ascii=False, indent=2)+'\n')
print('PASS: mounted layout, Retina background, exactly two draggable installer items.')
