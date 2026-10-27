# Building Voicemore

## Requirements

- Apple Silicon Mac with macOS 26.2 or later.
- Xcode Command Line Tools, Go 1.27.1 or later, Python 3 and Git.
- The signing identity required by `internal/distribution/policy.json` for app packaging. Certificates and private keys are not included.

Go dependencies are pinned in `go.mod` and `go.sum`. Local ASR dependencies and model/runtime manifests live in `internal/localasr/assets/`. Model weights and runtime archives are downloaded separately.

## Compile the source

```sh
git clone https://github.com/wherexml/Voicemore.git
cd Voicemore
mkdir -p build
go build -o build/voicemore-source .
```

This checks client compilation. The binary alone is not a signed, packaged macOS application and does not establish native permission or dictation behavior.

## Development application

```sh
python3 scripts/build-development.py
```

The packaging script requires the project's configured Apple Development identity; it stops if that identity is unavailable. It builds **Voicemore Dev.app**, with a separate bundle ID, `Voice Input Dev` data directory and no production update feed. Output is under `build/development/`.

## Distribution

```sh
./scripts/fetch-asr-runtime.sh
./scripts/build-macos.sh
```

These commands verify the pinned runtime and prepare a production candidate using the approved Developer ID identity, Apple notarization and final-package verification. They require the maintainer's signing and notarization setup. Missing credentials stop the workflow; there is no unsigned or ad-hoc fallback.

The explicit release entry point is `scripts/publish-release.sh`; use `--help` for options. Candidates are not published by default. Stable releases must pass signature, notarization, stapled-ticket, Gatekeeper and final-installer checks. Signed update archives, manifests, checksums and the pinned runtime asset are part of the release.

## Verification

Native acceptance runners are in `cmd/`; the `scripts/verify-*.sh` and `scripts/verify-*.py` entry points cover input, dictionary behavior, persistence and distribution artifacts. Review each runner's inputs before using it: some require permissions, fixtures or configured services.

For dictation changes, exercise the packaged development app with an actual input field, verify the inserted text and history, and check quit/relaunch behavior. Record the build, environment, inputs, expected and observed results, and repeatable evidence under the Git-ignored `artifacts/` directory. A successful build is not a native end-to-end test.

## Repository scope

Keep client source, required resources, dependency pins, build/acceptance tools and [third-party notices](resources/licenses/README.md) in Git. Website sources, deployment state, internal specifications, agent instructions, service patches, experimental prototypes and personal acceptance evidence stay local. These files are not required to compile the client.
