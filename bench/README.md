# voxbench corpora

Two corpora, for two different questions.

## libri (reproducible)

A LibriSpeech test-clean subset. Anyone can regenerate it and get the same
numbers, which makes it the right corpus for comparing against published WER
and for catching regressions in CI.

```bash
make bench-libri
```

Clean read speech from audiobooks. It does not resemble how people actually
dictate, so treat it as a floor rather than a prediction.

## personal (representative)

Your own dictation, recorded through vox's own audio path, with hand-corrected
transcripts. This is the corpus that decides whether an engine is actually
better for vox, because it carries the real microphone, the real room, the
real technical vocabulary, and the real cadence.

Record 20 to 50 clips:

```bash
mkdir -p bench/personal/clips
rec -q -r 16000 -c 1 -b 16 -t wav bench/personal/clips/0001.wav
```

Then write `bench/personal/manifest.jsonl` with one line per clip:

```json
{"wav": "clips/0001.wav", "text": "exactly what you said, corrected by hand"}
```

Run it:

```bash
make bench-personal
```

Neither corpus is committed. Audio and manifests are gitignored; only the
generated `results-*.md` reports are worth sharing.
