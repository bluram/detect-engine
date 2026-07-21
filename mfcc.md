# MFCC: How a Computer Turns Sound into a Fingerprint

## What is MFCC, and why should you care?

**MFCC** stands for **Mel-Frequency Cepstral Coefficients**. Behind the intimidating name is a simple goal: take raw audio — millions of meaningless numbers — and boil each tiny slice of it down to about **12 numbers** that capture what the sound actually *is*. Its **timbre**. The thing that makes a violin a violin and one advertisement different from another.

A good fingerprint needs three properties, and every step of MFCC exists to serve them:

- **Compact** — small enough to compare millions of times quickly.
- **Robust** — barely changes when the sound is louder, quieter, or a little noisy.
- **Comparable** — two fingerprints can be scored for similarity with a single number.

This is the engine behind **speech recognition**, **music identification**, and **audio detection** — for example, finding a 15-second advertisement buried inside an hour of noisy radio.

Here's the whole pipeline. We'll walk through each step: what it does, *why*, and the Go code.

```
raw samples
   │
1. Frame     cut into short overlapping slices (~23 ms)
2. Window    smooth the edges of each slice
3. FFT       find which frequencies are inside   (Fourier)
4. Mel       group frequencies the way the ear does
5. Log       energy → loudness (like decibels)
6. DCT       compress into ~12 "shape" numbers    (the cepstrum)
7. Binarize  reduce to 0s and 1s for fast matching
   │
the fingerprint
```

---

## Step 1 — Frame

A sound is never one single thing. Over a few seconds it's speech, then music, then a pause. If you try to analyze the whole recording at once, all of that smears together into meaningless mush.

The trick is to zoom in. Inside a tiny slice of about **23 milliseconds**, the sound barely changes — it's almost frozen. So the very first step is to chop the long list of samples into short slices called **frames**. It's exactly like film: a movie feels continuous, but it's really a stack of still frames, each capturing one frozen instant.

There's one subtlety: the frames **overlap** (usually by half). If they sat edge-to-edge, an important sound landing right on a boundary could get split and lost. Overlapping means every moment is fully captured inside at least one frame.

Why 23 ms specifically? It falls out of the sample rate. At 22,050 samples per second, a frame of 512 samples lasts `512 / 22050 ≈ 23 ms` — long enough to contain real frequency information, but short enough that the sound inside hasn't changed yet.

```go
// Frames cuts a signal into short overlapping frames.
func Frames(signal []float64, frameLen, frameStep int) [][]float64 {
    var frames [][]float64
    for start := 0; start+frameLen <= len(signal); start += frameStep {
        frames = append(frames, signal[start:start+frameLen])
    }
    return frames
}
```

---

## Step 2 — Window (Hamming)

We just cut a frame out of the signal — and that innocent act creates a problem. At the two ends of the frame, the wave gets sliced off abruptly, leaving sharp edges.

Those edges matter because of what the next step assumes. The FFT treats each frame as if it **repeats forever**, looped end-to-end. If the value at the start of the frame doesn't match the value at the end, the loop has a sudden jump at the seam — and a sudden jump looks, to the FFT, like a burst of high frequencies that were never really in the sound. This phantom effect is called **spectral leakage**. Imagine splicing the two ends of a tape into a loop: if they don't line up, you hear a click on every pass.

The fix is a **window**: we multiply the frame by a gentle bell-shaped curve that fades both edges down to nearly zero while leaving the middle untouched. Now both ends meet at zero, the loop is seamless, and the fake frequencies vanish. The most common bell is the **Hamming window**.

```go
// Hamming builds the bell: ~0.08 at the edges, ~1 in the middle.
func Hamming(n int) []float64 {
    w := make([]float64, n)
    for i := range w {
        w[i] = 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(n-1))
    }
    return w
}

func ApplyWindow(frame, window []float64) []float64 {
    out := make([]float64, len(frame))
    for i := range frame {
        out[i] = frame[i] * window[i]
    }
    return out
}
```

---

## Step 3 — FFT (finding the frequencies)

Here's where the sound reveals its identity. A violin and a trumpet playing the exact same note still sound completely different — because each one is really a *stack of frequencies*, and the recipe of frequencies is what your brain recognizes. So the central question is: **which frequencies are inside this frame, and how much of each?**

The Fourier transform answers it with a beautifully simple trick. To test whether a certain frequency is present, you take a pure test wave of that frequency, multiply it point-by-point with the signal, and add everything up. If that frequency really is in the sound, the two rise and fall *in step*, the products stay positive, and the sum is large. If it isn't, they drift in and out of step, the products swing between positive and negative, and everything **cancels to nearly zero**. Do this for every frequency and you get the full spectrum.

Why a *sine* wave as the test? Because a sine is the one wave that contains a single, pure frequency and nothing else — it's the true atom of sound. (Any other shape is already a mixture of sines.) In practice we test with both a sine and a cosine so we catch the frequency no matter its timing, and combine them — which is where complex numbers quietly enter the formula.

The **FFT** ("Fast Fourier Transform") is just a clever algorithm that computes all of this far faster than the brute-force way. We finish by taking the **power spectrum** — the energy at each frequency. A 512-sample frame yields 257 frequency values, spaced evenly from 0 Hz up to half the sample rate (the **Nyquist** limit).

```go
// PowerSpectrum returns the energy at each frequency (|FFT|^2).
func PowerSpectrum(frame []float64) []float64 {
    n := len(frame)
    buf := make([]complex128, n)
    for i, v := range frame {
        buf[i] = complex(v, 0)
    }
    FFT(buf)

    power := make([]float64, n/2+1)
    for k := range power {
        re, im := real(buf[k]), imag(buf[k])
        power[k] = re*re + im*im
    }
    return power
}
```

---

## Step 4 — Mel filterbank (hearing like a human)

After the FFT we have 257 frequency values, spaced perfectly evenly in Hz. The problem is that a human ear is anything *but* even.

We hear low frequencies with fine detail and high frequencies coarsely. The gap between 100 Hz and 200 Hz sounds enormous — a whole octave. But 8,000 Hz and 8,100 Hz are indistinguishable, even though the Hz difference is the same. Our perception of pitch is stretched at the bottom and squished at the top: each octave *doubles* the frequency (110 → 220 → 440 Hz), so equal steps to the ear mean multiplying, not adding.

The **mel scale** captures exactly this. We build a set of **26 triangular filters** spaced evenly on the mel scale — which means, in real Hz, they come out **narrow and dense at low frequencies** (fine detail where we hear it) and **wide and sparse at high frequencies** (coarse where we don't). Each triangle covers a band, and we simply add up the spectrum's energy inside it.

A crucial point: we are **not throwing high frequencies away**. 8,000 Hz is perfectly audible. We just *sum* the many high frequencies we can't tell apart into a single band, instead of keeping each one separately. Nothing is lost — the energy is preserved, only at a resolution that matches the ear. This does three things at once: it makes the numbers match human hearing, it compresses 257 values down to 26, and it adds robustness, because small frequency shifts and noise get averaged away inside each band.

```go
// MelEnergies groups the power spectrum into mel bands.
// Each band is a triangular filter's weighted sum of the spectrum.
func MelEnergies(power []float64, filterbank [][]float64) []float64 {
    energies := make([]float64, len(filterbank))
    for b, filter := range filterbank {
        var sum float64
        for k, weight := range filter {
            sum += power[k] * weight // multiply-and-sum
        }
        energies[b] = sum
    }
    return energies
}
```

---

## Step 5 — Log

After the mel step we have 26 numbers per frame — the energy in each band. But a problem is hiding in them, and fixing it is the whole point of this step.

Sound energy has a *massive* range: the quiet and loud parts of a recording can differ by a factor of millions. Those raw energies are unwieldy — and worse, they change completely when you just turn the volume up or down. That's a disaster for a fingerprint: the same ad played louder must not produce different numbers.

The fix is to take the **logarithm** of each energy. A logarithm turns big multiplications into small additions — `10 → 1`, `100 → 2`, `1000 → 3`: every time the input is ×10, the output grows by just +1, so the huge range collapses into small, even numbers.

But the real magic is what it does to **volume**. When a sound gets louder, every band's energy is *multiplied* by the same factor — and `log(a × x) = log(a) + log(x)`. So a louder version of the same sound is just the same 26 numbers with the **same constant added to all of them**; the *shape* is untouched. Since the later steps only look at the shape (the differences between the numbers), the volume simply cancels out. That's why the fingerprint recognizes an ad whether it's whispered or blasted.

And there's a bonus: this is also *how humans hear*. We perceive loudness logarithmically — 10× the energy sounds only about twice as loud. That's exactly what **decibels** are. So the log doesn't just tidy the math; it makes the numbers behave like a human ear.

*(One detail: `log(0)` is undefined, so we clamp tiny values to a small epsilon first.)*

```go
const epsilon = 1e-10 // guard against log(0)

for i, e := range energies {
    if e < epsilon {
        e = epsilon
    }
    energies[i] = math.Log(e)
}
```

---

## Step 6 — DCT (the cepstrum)

We're down to 26 log-mel numbers per frame, but they're still not ideal. Picture them as a bumpy curve across the bands — and notice that neighboring points tend to repeat each other (if one band is loud, its neighbor usually is too). There's redundancy, and there's fine jitter that's mostly noise. We want fewer numbers that keep the essence.

The **DCT** (Discrete Cosine Transform) does this elegantly. It rewrites that bumpy 26-point curve as a **sum of waves, from slow to fast**: a slow wave that captures the overall tilt and shape, a slightly faster one for the broad humps, and increasingly fast ones for the tiny wiggles. The **slow waves carry the overall shape** — the sound's identity — while the **fast waves carry the little wiggles** — mostly noise. So we simply keep the first ~12 and discard the rest. It's like sketching a face: "round face, big nose, wide smile" is enough to recognize someone; you don't need to draw every pore.

We also drop the very first coefficient, which is just the overall level (loudness) — one final nudge toward being volume-independent. And the name? Running a transform on a *log spectrum* produces what's playfully called a **cepstrum** (rearrange *spectrum*). Its outputs are the **cepstral coefficients** — the **C** in MFCC. Now the whole name reads cleanly: **M**el-**F**requency **C**epstral **C**oefficients.

```go
// applyDCT turns 26 log-mel energies into cepstral coefficients.
func applyDCT(logMel []float64, dct [][]float64) []float64 {
    coeffs := make([]float64, len(dct))
    for r, row := range dct {
        var sum float64
        for i, w := range row {
            sum += logMel[i] * w
        }
        coeffs[r] = sum
    }
    return coeffs
}
```

Result: 26 numbers → **12 MFCCs** per frame. That is the finished MFCC.

---

## Step 7 — Binarize (for fast matching)

The 12 MFCCs are real numbers, and for pure feature extraction you could stop here. But if your goal is to *match* sounds — like finding an ad inside a long recording — one more step makes it dramatically faster and even more robust.

We reduce each coefficient to a single **bit**: mark **1** if it's above that band's own average over time, and **0** if it's below. This throws away the exact values and keeps only the *pattern* — "is this band hotter or cooler than usual right now?" That pattern is what carries the identity, and because it ignores absolute levels entirely, it shrugs off changes in volume and a good deal of noise. As a bonus, comparing two fingerprints becomes a matter of counting matching bits — extremely fast.

```go
// binarize marks 1 where a coefficient is above its band's average.
func binarize(coeffs [][]float64) [][]int8 {
    bits := make([][]int8, len(coeffs))
    for b, row := range coeffs {
        var sum float64
        for _, v := range row {
            sum += v
        }
        mean := sum / float64(len(row))

        out := make([]int8, len(row))
        for t, v := range row {
            if v > mean {
                out[t] = 1
            }
        }
        bits[b] = out
    }
    return bits
}
```

---

## Putting it all together

Every step lives inside one loop: cut into frames, then run each frame through window → FFT → mel → log → DCT, and finally binarize the whole grid. (The framing/windowing/spectrum helpers live in a `dsp` package; the mel/DCT/binarize steps in `fingerprint`.)

```go
func FromSamples(samples []float64, cfg Config) *Fingerprint {
    cfg = cfg.normalised()

    frames := dsp.Frames(samples, cfg.FrameLen, cfg.FrameStep)
    window := dsp.Hamming(cfg.FrameLen)
    filterbank := MelFilterbank(cfg.NumFilters, cfg.FrameLen, cfg.SampleRate, cfg.LowFreqHz, cfg.HighFreqHz)
    dct := dctMatrix(cfg.NumCoeffs, cfg.NumFilters)

    bands := cfg.NumCoeffs - 1 // drop coefficient 0 (loudness)
    coeffs := make([][]float64, bands)
    for b := range coeffs {
        coeffs[b] = make([]float64, len(frames))
    }

    for t, frame := range frames {
        windowed := dsp.ApplyWindow(frame, window)
        power := dsp.PowerSpectrum(windowed)
        logMel := MelEnergies(power, filterbank)
        for i, e := range logMel {
            if e < epsilon {
                e = epsilon
            }
            logMel[i] = math.Log(e)
        }
        for b, c := range applyDCT(logMel, dct) {
            coeffs[b][t] = c
        }
    }

    return &Fingerprint{
        Bits:       binarize(coeffs),
        SampleRate: cfg.SampleRate,
        HopSamples: cfg.FrameStep,
    }
}
```

---

## Summary

| Step | Input → Output | In one line |
|------|----------------|-------------|
| Frame | samples → many frames | slice sound into ~23 ms pieces where it barely changes |
| Window | frame → smoothed frame | fade the edges so the FFT doesn't invent fake frequencies |
| FFT | frame → 257 frequencies | find which frequencies are inside, via multiply-and-sum |
| Mel | 257 → 26 bands | group frequencies like the ear: fine at low, coarse at high |
| Log | 26 → 26 | energy → loudness; makes it independent of volume |
| DCT | 26 → 12 | keep the smooth shape (identity), drop the wiggles (noise) |
| Binarize | 12 → bits | keep only the pattern, for fast and robust matching |

MFCC works because it mirrors how we actually *perceive* sound. It keeps the frequencies and loudness that carry a sound's identity, and deliberately throws away everything else — the exact volume, the inaudible detail, the noise. That's why a fingerprint of just ~12 numbers per slice can recognize a single advertisement hidden inside an hour of noisy radio, no matter how loud it happens to be playing.
