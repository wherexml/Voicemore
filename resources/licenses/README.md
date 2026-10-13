# Third-party notices

Voicemore uses MyGo, purego, coder/websocket, go-text/typesetting and golang.org/x packages. Their license texts are included in the neighboring directories, copied from the pinned Go modules.

Provider marks are used to identify their respective services; no endorsement is implied. See `../providers/README.md` for artwork sources. The Lobe Icons package is MIT licensed; the oMLX repository is Apache-2.0 licensed. Relevant license texts are included here.

The optional local inference runtime includes Python and the packages pinned in `internal/localasr/assets/requirements.lock`. The runtime archive retains their distribution metadata and license files. Model weights are downloaded separately from the pinned `mlx-community/Qwen3-ASR-1.7B-8bit` revision; consult the model repository and upstream Qwen3-ASR license before redistribution or use.

The embedded Silero VAD model is distributed under MIT; see the included upstream license. Third-party licenses apply to their respective components and do not grant rights to third-party trademarks.
