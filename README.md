# detect-engine

Find where a short **reference** clip occurs inside a long **record** — a
Shazam-style media matcher, written from scratch in Go.

The engine is **domain-neutral**: it doesn't know or care what the media *is*
(a song, a movie, a jingle, a spoken phrase …). You give it a record and a set
of references; it reports the timestamps where each reference occurs in the
record, each with a confidence score.

It matches on **two independent modalities**:

- **audio** — an MFCC fingerprint of the *sound* (radio, or the sound track of
  a video).
- **video** — a dHash (perceptual hash) fingerprint of the *picture*.

Both modalities produce the **same** binary fingerprint type, so one matcher
and one on-disk format serve both. A recording is fingerprinted once, then
matched against many references.

```
  Reference                  Time       Audio     Video     Verdict
  ──────────────────────────────────────────────────────────────────────────
  wolt-popysia               10:14.3    0.791     0.865     review
  Investaz_ozune_yatirim_et… 10:40.4    0.788     0.825     review
  Clear_man_-_Fifa_resmi_sp… 12:42.6    0.797     0.800     review
  Via_Uomo_-_kisi_geyimleri… 36:49.8    0.791     0.892     review
```

---

## How it works

Every file is turned into a **fingerprint**: a grid of 1s and 0s
(`[band][frame]`) that captures the *shape* of the media over time while
ignoring loudness, colour, resolution and noise. The reference's fingerprint is
slid across the record's; wherever they line up strongly enough, that's a match.

Two pipelines fill the same grid:

```
audio:  decode → frame → window → FFT power → mel bands → log → DCT → binarize ┐
                                                                                 ├─→ fingerprint
video:  decode frames → shrink + grayscale → dHash (pixel > right neighbour) ───┘        │
                                                                                          │
                                              slide (FFT cross-correlation) ──────────────┘→ matches
```

- **Audio (MFCC).** ffmpeg decodes the sound to samples; each short frame is
  windowed, transformed to a power spectrum, mapped onto mel bands, log-scaled,
  DCT'd, and binarized. See [`mfcc.md`](mfcc.md) for the full recipe.
- **Video (dHash).** ffmpeg samples 3 frames/second and scales each down to a
  tiny 9×8 grayscale image. Within each frame every pixel is compared to its
  right neighbour (brighter → 1, darker → 0), giving 64 bits per frame. The
  shrink discards resolution (360p and 720p of the same clip hash alike) and
  grayscale drops colour, keeping only the stable brightness structure.

A match must clear **three** independent bars, which keeps false positives rare:

1. **Stands out** — a peak above the record's own noise floor (`mean + k·std`).
2. **Strong enough** — overall agreement above an absolute floor (`MinScore`).
3. **Consistent** — every one-second block agrees (`MinSecondScore`), so a
   spike that only matches on average is rejected.

### Timestamps stay accurate, even through recording gaps

Match times are reported against the record's **real timeline**. This matters
because live/broadcast captures often contain **gaps** — a handful of samples, or
several seconds, dropped during recording (a network hiccup, an overloaded
capture box). A naive decode packs the surviving audio contiguously and
**collapses** those gaps, so every timestamp after a gap drifts *earlier* than
reality — a match at 14:14 in the file would be reported as 14:08.

To prevent that, the decoder fills gaps with silence and anchors the start
(ffmpeg's `aresample=async`), so decoded-sample-time equals real (container)
time. The fingerprint's timeline therefore stays locked to the recording's own
clock, and reported offsets are the true positions in the file.

---

## Architecture

Each package has one job, and dependencies flow one way
(`cmd → matcher → fingerprint → dsp`; nothing points back).

```
detect-engine/                  (module: github.com/bluram/detect-engine)
├── cmd/                        the programs (one folder = one binary)
│   ├── detect/                 SEARCH app: find references in a record
│   └── fingerprinter/          FINGERPRINT app: media file → .bin fingerprint(s)
└── internal/                   the shared engine (both apps reuse it)
    ├── deps/                   checks external tools (ffmpeg) are installed
    ├── audio/                  decode sound → float samples (via ffmpeg) → MFCC fingerprint
    ├── video/                  decode frames → grayscale → dHash fingerprint
    ├── dsp/                    signal tools: FFT + real FFT (Plan/RealPlan), Frames, Hamming, PowerSpectrum
    ├── fingerprint/            the media-neutral Fingerprint type (+ .bin Save/Load)
    └── matcher/                slide fingerprints, detect matches (reference in record)
```

- **`cmd/`** holds the programs; **`internal/`** holds the engine they share.
- **`dsp`** knows nothing about fingerprints — it's reusable signal math.
- **`fingerprint`** is the shared bit grid + timing metadata; both `audio` and
  `video` produce it, and it saves/loads `.bin`.
- **`audio`** builds the MFCC fingerprint; **`video`** builds the dHash
  fingerprint. They differ only in how the bit grid is filled.
- **`matcher`** compares two fingerprints; it speaks only *reference* and
  *record*, and is blind to both the domain (song, movie, …) and the modality
  (audio or video).

To match **many references against one record**, matching uses cache-friendly
**overlap-save** block correlation with a **real FFT** — many small transforms
that stay in the CPU cache — instead of one giant record-length transform, so it
stays fast.

---

## Build & run

Requires **Go 1.22+** and **ffmpeg** (and **ffprobe**) on PATH.

```bash
# build both programs
go build -o bin/detect         ./cmd/detect
go build -o bin/fingerprinter  ./cmd/fingerprinter
```

The workflow is two steps: **fingerprint once**, then **search many times**.

### 1. Fingerprint (touches ffmpeg)

The fingerprinter auto-detects each file's tracks and writes one `.bin` per
modality next to the source: a video file yields `<file>.audio.bin` **and**
`<file>.video.bin`; an audio-only file yields just `<file>.audio.bin`.

```bash
# a single file (auto by content)
./bin/fingerprinter -in records/video/rec.mp4     # → rec.mp4.audio.bin + rec.mp4.video.bin
./bin/fingerprinter -in references/audio/ad.mp3   # → ad.mp3.audio.bin

# a whole folder
./bin/fingerprinter -in-dir ./references/video

# restrict what gets written
./bin/fingerprinter -in records/video/rec.mp4 -type audio        # audio only
./bin/fingerprinter -in records/video/rec.mp4 -type audio,video  # both
```

Up-to-date `.bin` files are skipped unless `-force` is given.

### 2. Search (loads `.bin` only — no ffmpeg, no decoding)

`detect` takes a `-record` (a media path or a fingerprint base — the suffix is
stripped either way) and a `-references` folder. `-mode` says what to match:

```bash
# radio: match sound only → one audio score
./bin/detect -mode audio -record records/audio/rec.mp3 -references references/audio

# TV: match picture AND sound → both scores + a verdict
./bin/detect -mode video -record records/video/rec.mp4 -references references/video
```

Given `-record rec.mp4`, detect loads `rec.mp4.audio.bin` (and, in video mode,
`rec.mp4.video.bin`), and pairs each reference's `.audio.bin` ↔ `.video.bin` by
base name. In **video** mode the audio side still runs (a video has both
tracks), so both scores are reported for every air-time.

Each program is a single self-contained binary (no external Go libraries).

### Flags

**`detect`**

| Flag | Default | Meaning |
|------|---------|---------|
| `-mode` | — | `audio` (sound only) or `video` (picture + sound). **Required.** |
| `-record` | — | recording media path or fingerprint base. **Required.** |
| `-references` | — | folder of reference fingerprints. **Required.** |
| `-json` | off | emit results as JSON instead of a table |

**Exit codes:** `0` = at least one detection · `1` = nothing found · `2` =
usage/error.

**`fingerprinter`**

| Flag | Default | Meaning |
|------|---------|---------|
| `-in` | — | a single media file to fingerprint |
| `-in-dir` | — | a folder of media files to fingerprint |
| `-out` | — | output path (only valid with a single `-in` and a single `-type`) |
| `-type` | auto | restrict output: `audio`, `video`, or `audio,video` |
| `-force` | off | re-fingerprint even if an up-to-date `.bin` exists |

### Reading the numbers

- **Audio / Video score** — fraction of fingerprint cells that agree (1.0
  identical, 0.5 unrelated). Even a near-exact match tops out around
  **0.80–0.86** — frame alignment and re-encoding cost the rest — so 0.8+
  already means "definitely it". A match with a different background bed or a
  re-cut picture can sit lower and still be real. **No score is hidden:** when
  one modality clears the filters but the other doesn't, the other side's actual
  score at that time is looked up and still shown, so a variant (same sound,
  changed picture, or vice versa) is visible instead of a blank. A dash (`–`)
  means there is no reference for that side at all.
- **Verdict** — one word summarising the sides that actually detected:
  `confident` (both high) · `review` (likely, worth a human check) · `low`.
  Looked-up sub-threshold scores don't count as confirmations.

The matcher's detection thresholds (`MinScore` 0.62, `MinSecondScore` 0.40,
peak `≥ mean + 5·std`) live in `matcher.DefaultParams()`.

---

## Tests

```bash
go test ./...
```

Covers the FFT round-trip, spectrum peak detection, fingerprint determinism,
the dHash fingerprint, the `.bin` store round-trip, and an end-to-end matcher
check (a reference embedded in a record is found at the right offset, including
many references against one prepared record).
</content>
</invoke>
