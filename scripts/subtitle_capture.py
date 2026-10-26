"""Build and sign VoicemoreAudioTap, the live-subtitle system-audio helper, into a source tree.

It goes to resources/subtitles/ of the build copy (never the checkout), so `mygo build` places it in
Contents/Resources/subtitles/ and seals it with the app. It runs as the app's child process, so the
system audio recording permission belongs to the app, whose Info.plist carries
NSAudioCaptureUsageDescription. Source: internal/subtitles/capture/main.swift.
"""
from pathlib import Path
import subprocess

SOURCE = 'internal/subtitles/capture/main.swift'
OUTPUT = 'resources/subtitles/VoicemoreAudioTap'


def build(tree, identity, identifier, release):
    tree = Path(tree)
    out = tree / OUTPUT
    out.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(['xcrun', 'swiftc', '-O', '-swift-version', '5', '-target', 'arm64-apple-macos14.2',
                    '-framework', 'CoreAudio', '-framework', 'AudioToolbox', '-framework', 'AVFoundation',
                    str(tree / SOURCE), '-o', str(out)], check=True)
    sign = ['codesign', '--force', '--sign', identity, '--identifier', identifier,
            '--entitlements', str(tree / 'resources/VoiceInput.entitlements')]
    # Distribution needs the hardened runtime and a secure timestamp for notarization.
    sign += ['--options', 'runtime', '--timestamp'] if release else ['--timestamp=none']
    subprocess.run(sign + [str(out)], check=True)
    subprocess.run(['codesign', '--verify', '--strict', str(out)], check=True)
    return out


def verify_bundled(app):
    tap = Path(app) / 'Contents/Resources/subtitles/VoicemoreAudioTap'
    if not tap.is_file():
        raise SystemExit(f'Subtitle capture helper missing from the bundle: {tap}')
    subprocess.run(['codesign', '--verify', '--strict', str(tap)], check=True)
    return tap
