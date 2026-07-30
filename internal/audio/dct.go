package audio

import "math"

// dctMatrix precomputes an orthonormal DCT-II basis used to compress the mel
// band energies into cepstral coefficients. It returns rows for output
// coefficients 1..numCoeffs-1 (coefficient 0, which only reflects overall
// loudness, is intentionally skipped) — each row has numFilters weights.
func dctMatrix(numCoeffs, numFilters int) [][]float64 {
	rows := numCoeffs - 1
	scale := math.Sqrt(2.0 / float64(numFilters))

	matrix := make([][]float64, rows)
	for r := range rows {
		k := float64(r + 1) // +1 because coefficient 0 is skipped
		row := make([]float64, numFilters)
		for i := range numFilters {
			row[i] = scale * math.Cos(math.Pi*k*(2*float64(i)+1)/(2*float64(numFilters)))
		}
		matrix[r] = row
	}
	return matrix
}

// applyDCT projects one frame's log-mel energies onto the DCT basis, producing
// the cepstral coefficients (one value per matrix row).
func applyDCT(logMel []float64, matrix [][]float64) []float64 {
	out := make([]float64, len(matrix))
	for r, row := range matrix {
		var sum float64
		for i, w := range row {
			sum += logMel[i] * w
		}
		out[r] = sum
	}
	return out
}
