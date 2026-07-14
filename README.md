# detect-engine

Find where a short **query** clip occurs inside a long **record** — a
Shazam-style media matcher, written from scratch in Go.

The engine is **domain-neutral**: it doesn't know or care what the audio *is*
(a song, a movie, a jingle, a spoken phrase …). You give it two files; it
reports the timestamps where the query occurs in the record, each with a
confidence score.

```
detect-engine -record record.mp3 -query clip.mp3
```

```
  Record  : record.mp3
  Queries : 1 searched

  ── clip.mp3                  ✓ 3 match(es)
         1  17:55.0 → 18:22.0  (27.0s)   score 0.855   worst/sec 0.834   high
         2  35:36.8 → 36:03.8  (27.0s)   score 0.835   worst/sec 0.750   medium
         3  47:11.5 → 47:38.4  (27.0s)   score 0.813   worst/sec 0.742   medium
```

Both inputs may be **audio or video** — ffmpeg extracts the audio track from
video automatically.

---

## How it works

Both files are turned into a **fingerprint**: a grid of 1s and 0s (MFCC) that
captures the *shape* of the sound over time while ignoring loudness and noise.
The query's fingerprint is slid across the record's; wherever they line up
strongly enough, that's a match.

```
decode → frame → window → FFT power spectrum → mel bands → log → DCT → binarize → fingerprint
                                                                                      │
                                          slide (FFT cross-correlation) ─────────────┘→ matches
```

A match must clear **three** independent bars, which keeps false positives rare:

1. **Stands out** — a peak above the record's own noise floor (`mean + k·std`).
2. **Strong enough** — overall agreement above an absolute floor (`-min-score`).
3. **Consistent** — every one-second block agrees (`-min-second-score`), so a
   spike that only matches on average is rejected.

---

## Architecture

Each package has one job, and dependencies flow one way
(`cli → matcher → fingerprint → dsp`; nothing points back).

```
detect-engine/                  (module: github.com/bluram/detect-engine)
├── main.go                     thin entry point → cli.Run
└── internal/
    ├── deps/                   checks external tools (ffmpeg) are installed
    ├── audio/                  decode media → float samples (via ffmpeg) + silence trim
    ├── dsp/                    generic signal tools: FFT, Frames, Hamming window, PowerSpectrum
    ├── fingerprint/            the MFCC recipe → Fingerprint (mel filterbank, DCT, binarize)
    ├── matcher/                slide fingerprints, detect matches (query in record)
    └── cli/                    flag parsing, wiring, human/JSON output
```

- **`dsp`** knows nothing about fingerprints — it's reusable signal math.
- **`fingerprint`** builds on `dsp` and produces the fingerprint.
- **`matcher`** compares two fingerprints; it speaks only *query* and *record*,
  never the domain (song, movie, …).
- **`cli`** is the composition root that wires everything together.

To match **many queries against one record**, `matcher.Prepare` transforms the
record once and reuses it for every query.

---

## Build & run

Requires **Go 1.21+** and **ffmpeg** on PATH.

```bash
go build -o detect-engine .
./detect-engine -record record.mp3 -query clip.mp3          # one query
./detect-engine -record record.mp3 -queries-dir ./clips     # many queries, one record
```

The result is a single self-contained binary (no external Go libraries).

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-record` | — | the record to search in (audio or video). **Required.** |
| `-query` | — | a single query to find (audio or video) |
| `-queries-dir` | — | a directory of queries to search against the record |
| `-min-score` | `0.62` | absolute floor on overall agreement (0–1) |
| `-min-second-score` | `0.40` | every 1-second block must agree at least this much |
| `-min-gap` | query×0.8 | minimum seconds between two matches |
| `-json` | off | machine-readable output |
| `-v` | off | verbose logging to stderr |

Provide `-query` or `-queries-dir` (at least one). **Exit codes:** `0` = found ·
`1` = not found · `2` = usage/error.

### Reading the numbers

- **score** — fraction of fingerprint cells that agree (1.0 identical, 0.5
  unrelated). Clean matches land 0.80–0.95; a match with a different background
  bed can sit 0.62–0.72 (the **review** band) and still be real.
- **worst/sec** — the weakest one-second block in the match.
- **confidence** — `high` / `medium` / `review` / `low`. Treat **review** as
  "likely, worth a human ear", not "rejected".

---

## Tests

```bash
go test ./...
```

Covers the FFT round-trip, spectrum peak detection, fingerprint determinism, and
an end-to-end matcher check (a query embedded in a record is found at the right
offset, including many queries against one prepared record).
