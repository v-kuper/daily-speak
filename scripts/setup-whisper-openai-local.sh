#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

PYTHON_BIN="${PYTHON_BIN:-python3}"
VENV_DIR="${VENV_DIR:-$ROOT_DIR/.venv}"
WHISPER_MODEL_DIR="${WHISPER_MODEL_DIR:-$ROOT_DIR/tools/whisper/openai-models}"
WHISPER_CACHE_DIR="${WHISPER_CACHE_DIR:-$ROOT_DIR/tools/whisper/cache}"
PIP_CACHE_DIR="${PIP_CACHE_DIR:-$ROOT_DIR/tools/whisper/pip-cache}"
FFMPEG_DIR="${FFMPEG_DIR:-$ROOT_DIR/tools/ffmpeg}"
FFMPEG_LINK_DIR="$FFMPEG_DIR/bin"
FFMPEG_LINK_PATH="$FFMPEG_LINK_DIR/ffmpeg"

mkdir -p "$WHISPER_MODEL_DIR" "$WHISPER_CACHE_DIR" "$PIP_CACHE_DIR" "$FFMPEG_LINK_DIR"

if [ ! -d "$VENV_DIR" ]; then
  "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

"$VENV_DIR/bin/python" -m pip install -U pip
PIP_CACHE_DIR="$PIP_CACHE_DIR" "$VENV_DIR/bin/pip" install -U openai-whisper imageio-ffmpeg

# Download a local ffmpeg binary via imageio-ffmpeg and expose it as tools/ffmpeg/bin/ffmpeg.
FFMPEG_SOURCE="$(IMAGEIO_USERDIR="$FFMPEG_DIR" "$VENV_DIR/bin/python" - <<'PY'
import imageio_ffmpeg
print(imageio_ffmpeg.get_ffmpeg_exe())
PY
)"

ln -sfn "$FFMPEG_SOURCE" "$FFMPEG_LINK_PATH"
chmod +x "$FFMPEG_SOURCE" "$FFMPEG_LINK_PATH"

# Warmup: verifies module import and prints CLI help.
XDG_CACHE_HOME="$WHISPER_CACHE_DIR" \
TRANSFORMERS_CACHE="$WHISPER_CACHE_DIR/transformers" \
HF_HOME="$WHISPER_CACHE_DIR/hf" \
"$VENV_DIR/bin/python" -m whisper --help >/dev/null

LOCAL_FFMPEG="$FFMPEG_LINK_PATH"
if [ -x "$LOCAL_FFMPEG" ]; then
  echo "Local ffmpeg ready: $LOCAL_FFMPEG"
else
  echo "Local ffmpeg setup failed."
  exit 1
fi

echo
echo "Whisper local setup completed."
echo "Add to .env.local:"
echo "WHISPER_BACKEND=openai"
echo "WHISPER_PYTHON_BIN=.venv/bin/python"
echo "WHISPER_OPENAI_MODEL=base.en"
echo "WHISPER_OPENAI_MODEL_DIR=tools/whisper/openai-models"
echo "WHISPER_OPENAI_CACHE_DIR=tools/whisper/cache"
echo "WHISPER_FFMPEG_BIN=tools/ffmpeg/bin/ffmpeg"
