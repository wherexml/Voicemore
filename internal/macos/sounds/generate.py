"""Generate the app's original, restrained readiness/completion cues.
Run with Python 3; no external assets or dependencies.
"""
import math
import struct
import wave
from pathlib import Path

RATE = 44100
ROOT = Path(__file__).resolve().parent
for name, frequency, duration in [('start', 660, 0.18), ('stop', 440, 0.21)]:
    samples = []
    count = round(RATE * duration)
    for i in range(count):
        t = i / RATE
        # A rounded attack and a fully faded tail prevent a sharp click.
        attack = math.sin(min(1, t / 0.006) * math.pi / 2) ** 2
        tail = math.sin(min(1, (duration - t) / 0.045) * math.pi / 2) ** 2
        envelope = attack * tail * math.exp(-t / 0.055)
        tone = math.sin(2 * math.pi * frequency * t)
        # A soft mallet-like body; a perfect fifth separates start and stop.
        tone += 0.18 * math.exp(-t / 0.030) * math.sin(2 * math.pi * frequency * 2 * t)
        samples.append(round(32767 * 0.32 * envelope * tone))
    samples[-1] = 0
    with wave.open(str(ROOT / (name + '.wav')), 'wb') as out:
        out.setparams((1, 2, RATE, count, 'NONE', 'not compressed'))
        out.writeframes(struct.pack('<' + 'h' * count, *samples))
