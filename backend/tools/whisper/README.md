# Local Whisper Setup

The backend supports two local STT backends:

- `openai` (Python package from https://github.com/openai/whisper)
- `cpp` (`whisper.cpp` binary)

## Option 1: Docker `openai/whisper` (Python)

The backend Docker runtime image installs Linux Python, `openai-whisper`, and
`ffmpeg`. From the repository root, Compose mounts `./backend/tools` into
`/app/tools`, so model files and cache persist on the Windows or macOS host
across container rebuilds. The web container never receives this mount.

```bash
WHISPER_BACKEND=openai
WHISPER_PYTHON_BIN=/opt/whisper/bin/python
WHISPER_OPENAI_MODEL=base
WHISPER_OPENAI_MODEL_DIR=/app/tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=/app/tools/whisper/cache
WHISPER_FFMPEG_BIN=/usr/bin/ffmpeg
WHISPER_OPENAI_DEVICE=cpu
WHISPER_OPENAI_FP16=false
WHISPER_LANGUAGE=auto
```

## Option 2: Host `openai/whisper` (Python)

1. Run the backend-owned installer from the repository root:

```bash
npm run setup:whisper
```

2. The script installs everything under `backend/`: `backend/.venv`, models and
   cache under `backend/tools/whisper`, and an ffmpeg link under
   `backend/tools/ffmpeg/bin/ffmpeg`.
3. Configure these paths for a Go process started from `backend/`:

```bash
WHISPER_BACKEND=openai
WHISPER_PYTHON_BIN=.venv/bin/python
WHISPER_OPENAI_MODEL=base
WHISPER_OPENAI_MODEL_DIR=tools/whisper/openai-models
WHISPER_OPENAI_CACHE_DIR=tools/whisper/cache
WHISPER_FFMPEG_BIN=tools/ffmpeg/bin/ffmpeg
WHISPER_LANGUAGE=auto
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

The root scripts change into `backend/` before resolving these relative paths.
If you launch the API from another working directory, use absolute paths.

## Option 3: `whisper.cpp`

Expected layout below is relative to `backend/` (from the repository root it is
`backend/tools/whisper/`):

```text
tools/whisper/
  bin/
    whisper-cli      # or main
  models/
    ggml-base.bin # multilingual model
```

Env:

```bash
WHISPER_BACKEND=cpp
WHISPER_BINARY_PATH=/absolute/path/to/whisper-cli
WHISPER_MODEL_PATH=/absolute/path/to/ggml-base.bin
WHISPER_LANGUAGE=auto
WHISPER_THREADS=4
WHISPER_TIMEOUT_MS=180000
```

Use a multilingual model without the `.en` suffix for mixed English-Russian
recordings. `WHISPER_LANGUAGE=auto` lets Whisper detect the spoken language;
the backend also supplies mixed-language context and preserves Russian words in
Cyrillic for the correction step.
