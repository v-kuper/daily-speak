#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

PYTHON_BIN="${WHISPER_PYTHON_BIN:-.venv/bin/python}"
MODEL_DIR="${WHISPER_OPENAI_MODEL_DIR:-tools/whisper/openai-models}"
CACHE_DIR="${WHISPER_OPENAI_CACHE_DIR:-tools/whisper/cache}"
FFMPEG_BIN="${WHISPER_FFMPEG_BIN:-tools/ffmpeg/bin/ffmpeg}"

if [[ "$PYTHON_BIN" != /* ]]; then
  PYTHON_BIN="$ROOT_DIR/$PYTHON_BIN"
fi
if [[ "$MODEL_DIR" != /* ]]; then
  MODEL_DIR="$ROOT_DIR/$MODEL_DIR"
fi
if [[ "$CACHE_DIR" != /* ]]; then
  CACHE_DIR="$ROOT_DIR/$CACHE_DIR"
fi
if [[ "$FFMPEG_BIN" != /* ]]; then
  FFMPEG_BIN="$ROOT_DIR/$FFMPEG_BIN"
fi

echo "python: $PYTHON_BIN"
if [ ! -x "$PYTHON_BIN" ]; then
  echo "ERROR: python binary is not executable"
  exit 1
fi

echo "model_dir: $MODEL_DIR"
echo "cache_dir: $CACHE_DIR"
mkdir -p "$MODEL_DIR" "$CACHE_DIR"

set +e
XDG_CACHE_HOME="$CACHE_DIR" \
TRANSFORMERS_CACHE="$CACHE_DIR/transformers" \
HF_HOME="$CACHE_DIR/hf" \
"$PYTHON_BIN" -m whisper --help >/tmp/whisper_help_out.txt 2>/tmp/whisper_help_err.txt
WHISPER_HELP_EXIT=$?
set -e

if [ "$WHISPER_HELP_EXIT" -ne 0 ]; then
  echo "ERROR: whisper python module is unavailable"
  sed -n '1,40p' /tmp/whisper_help_err.txt
  exit 1
fi

echo "whisper module: OK"

if [ -x "$FFMPEG_BIN" ]; then
  echo "ffmpeg (local): $FFMPEG_BIN"
  "$FFMPEG_BIN" -version | head -n 1
elif command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg (global): $(command -v ffmpeg)"
  ffmpeg -version | head -n 1
else
  echo "ERROR: ffmpeg not found (local or global)"
  exit 1
fi

echo "Whisper local diagnostics: OK"
