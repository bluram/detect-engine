package dsp

// Frame cuts a long sound into short overlapping pieces (~Xms)
// because in a small piece the sound hardly changes - and can be clearly understood further.
func Frames(signal []float64, frameLen, frameStep int) [][]float64 {
	var frames [][]float64
	for start := 0; start+frameLen <= len(signal); start += frameStep {
		frames = append(frames, signal[start:start+frameLen])
	}
	return frames
}
