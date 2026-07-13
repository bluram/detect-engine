# advert-detect

Find where a short **advert** airs inside a long **radio/TV recording** — a
Shazam-for-adverts, written from scratch in Go.

You give it two files; it reports the timestamps where the advert was broadcast,
each with a confidence score.

```
advert-detect -record recording.mp3 -advert advert.mp3
```

```
  Recording : recording.mp3
  Advert    : advert.mp3

  ✓ Found 3 airing(s):

     1  at 17:55.0   score 0.672   worst/sec 0.444   review
     2  at 35:36.7   score 0.663   worst/sec 0.436   review
     3  at 47:11.4   score 0.664   worst/sec 0.462   review
```

Both inputs may be **audio or video** — ffmpeg extracts the audio track from
video automatically.

---

## How it works

Both files are turned into a **fingerprint**: a grid of 1s and 0s (MFCC) that
captures the *shape* of the sound over time while ignoring loudness and noise.
The advert's fingerprint is then slid across the recording's; wherever they line
up strongly enough, that's an airing.

```
decode → frame → window → FFT power spectrum → mel bands → log → DCT → binarize → fingerprint
                                                                                      │
                                          slide (FFT cross-correlation) ─────────────┘→ detections
```

A detection must clear **three** independent bars, which keeps false positives
rare:

1. **Stands out** — a peak above the recording's own noise floor (`mean + k·std`).
2. **Strong enough** — overall agreement above an absolute floor (`-min-score`).
3. **Consistent** — every one-second block agrees (`-min-second-score`), so a
   spike that only matches on average is rejected.

---

## Architecture

The code is organised so each package has one job and dependencies flow one way
(`cli → matcher → fingerprint → dsp`; nothing points back).

```
advert-detect/
├── main.go                     thin entry point → cli.Run
└── internal/
    ├── deps/                    checks external tools (ffmpeg) are installed
    ├── audio/                   decode media → float samples (via ffmpeg) + silence trim
    ├── dsp/                     generic signal tools: FFT, Frames, Hamming window, PowerSpectrum
    ├── fingerprint/             the MFCC recipe → Fingerprint (mel filterbank, DCT, binarize)
    ├── matcher/                 slide fingerprints, detect airings
    └── cli/                     flag parsing, wiring, human/JSON output
```

- **`dsp`** knows nothing about fingerprints — it's reusable signal math.
- **`fingerprint`** builds on `dsp` and produces the fingerprint.
- **`matcher`** compares two fingerprints.
- **`cli`** is the composition root that wires everything together.

---

## Build & run

Requires **Go 1.21+** and **ffmpeg** on PATH.

```bash
go build -o advert-detect .
./advert-detect -record recording.mp3 -advert advert.mp3
```

The result is a single self-contained binary (no external Go libraries).

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-record` | — | recording to search (audio or video). **Required.** |
| `-advert` | — | advert to find (audio or video). **Required.** |
| `-min-score` | `0.62` | absolute floor on overall agreement (0–1) |
| `-min-second-score` | `0.40` | every 1-second block must agree at least this much |
| `-min-gap` | advert×0.8 | minimum seconds between two hits |
| `-json` | off | machine-readable output |
| `-v` | off | verbose logging to stderr |

**Exit codes:** `0` = found · `1` = not found · `2` = usage/error.

### Reading the numbers

- **score** — fraction of fingerprint cells that agree (1.0 identical, 0.5
  unrelated). Clean airings land 0.80–0.95; an airing with a different music bed
  can sit 0.62–0.72 (the **review** band) and still be a true match.
- **worst/sec** — the weakest one-second block in the match.
- **confidence** — `high` / `medium` / `review` / `low`. Treat **review** as
  "likely, worth a human ear", not "rejected".

---

## Tests

```bash
go test ./...
```

Covers the FFT round-trip, spectrum peak detection, fingerprint determinism, and
an end-to-end matcher check (an advert embedded in a recording is found at the
right offset).
