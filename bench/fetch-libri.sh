#!/usr/bin/env bash
# Fetch a small LibriSpeech test-clean subset and emit a voxbench manifest.
# Audio is converted to the 16 kHz mono 16-bit WAV that vox records.
set -euo pipefail

CLIPS="${CLIPS:-50}"
DEST="$(cd "$(dirname "$0")" && pwd)/libri"
ARCHIVE_URL="https://www.openslr.org/resources/12/test-clean.tar.gz"

if [ -f "$DEST/manifest.jsonl" ]; then
  echo "manifest already present at $DEST/manifest.jsonl"
  echo "delete it to re-fetch"
  exit 0
fi

command -v ffmpeg >/dev/null || { echo "ffmpeg is required"; exit 1; }

mkdir -p "$DEST/clips"
cd "$DEST"

if [ ! -f test-clean.tar.gz ]; then
  echo "downloading LibriSpeech test-clean (~346 MB)..."
  curl -L -o test-clean.tar.gz "$ARCHIVE_URL"
fi

if [ ! -d LibriSpeech ]; then
  echo "extracting..."
  tar xf test-clean.tar.gz
fi

echo "building manifest (${CLIPS} clips)..."
: > manifest.jsonl
count=0

# Each .trans.txt line is: <utterance-id> <UPPERCASE TRANSCRIPT>
while IFS= read -r trans; do
  dir="$(dirname "$trans")"
  while IFS= read -r line; do
    [ "$count" -ge "$CLIPS" ] && break 2
    id="${line%% *}"
    text="${line#* }"
    src="$dir/$id.flac"
    [ -f "$src" ] || continue

    out="clips/$id.wav"
    # -nostdin: the enclosing loop reads the .trans.txt list on stdin, and
    # ffmpeg would otherwise consume it, silently truncating the corpus.
    ffmpeg -nostdin -loglevel error -y -i "$src" -ar 16000 -ac 1 -c:a pcm_s16le "$out"

    # Lowercase the reference; voxbench normalizes too, but keeping the
    # manifest readable helps when eyeballing diffs.
    lower="$(printf '%s' "$text" | tr '[:upper:]' '[:lower:]')"
    printf '{"wav":"%s","text":%s}\n' "$out" \
      "$(printf '%s' "$lower" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')" \
      >> manifest.jsonl
    count=$((count + 1))
  done < "$trans"
done < <(find LibriSpeech -name '*.trans.txt' | sort)

echo "wrote $count clips to $DEST/manifest.jsonl"
