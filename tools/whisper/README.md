# Local Whisper Setup

The app supports two local STT backends:
- `openai` (Python package from https://github.com/openai/whisper)
- `cpp` (`whisper.cpp` binary)

## Option 1: Docker `openai/whisper` (Python)

The Docker runtime image installs Linux Python, `openai-whisper`, and `ffmpeg`.
Compose mounts `./tools` into `/app/tools`, so model files and cache persist on
the Windows or macOS host across container rebuilds.

```bash
WHISPER_BACKEND=openai
WHISPER_PYTHON_BIN=/opt/whisper/bin/python
WHISPER_OPENAI_MODEL=base.en
WHISPER_OPENAI_MODEL_DIR=/app/tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=/app/tools/whisper/cache
WHISPER_FFMPEG_BIN=/usr/bin/ffmpeg
WHISPER_OPENAI_DEVICE=cpu
WHISPER_OPENAI_FP16=false
WHISPER_LANGUAGE=en
```

## Option 2: Host `openai/whisper` (Python)

1. Run project-local installer from repository root:
```bash
npm run setup:whisper
```
2. Script installs local ffmpeg via `imageio-ffmpeg` and links it to `tools/ffmpeg/bin/ffmpeg`.
3. Configure env:
```bash
WHISPER_BACKEND=openai
WHISPER_PYTHON_BIN=.venv/bin/python
WHISPER_OPENAI_MODEL=base.en
WHISPER_OPENAI_MODEL_DIR=tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=tools/whisper/cache
WHISPER_FFMPEG_BIN=tools/ffmpeg/bin/ffmpeg
WHISPER_LANGUAGE=en
```

Optional:
```bash
WHISPER_OPENAI_DEVICE=cpu
WHISPER_OPENAI_FP16=false
WHISPER_THREADS=4
WHISPER_TIMEOUT_MS=180000
```

Quick diagnostics:

```bash
npm run check:whisper
```

## Option 3: `whisper.cpp`

Expected layout:

```text
tools/whisper/
  bin/
    whisper-cli      # or main
  models/
    ggml-base.en.bin # or another ggml model
```

Env:

```bash
WHISPER_BACKEND=cpp
WHISPER_BINARY_PATH=/absolute/path/to/whisper-cli
WHISPER_MODEL_PATH=/absolute/path/to/ggml-base.en.bin
WHISPER_LANGUAGE=en
WHISPER_THREADS=4
WHISPER_TIMEOUT_MS=180000
```
