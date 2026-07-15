package fingerprint

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// The .bin file format (serialization / сериализация of a Fingerprint):
//
//	magic       [4]byte = "DEFP"   (Detect-Engine FingerPrint)
//	version     uint8
//	sampleRate  int32
//	hopSamples  int32
//	numBands    int32
//	numFrames   int32
//	bits        packed 8 per byte (numBands*numFrames bits total)
//
// The header lets us reject a wrong or outdated file instead of loading garbage.
const (
	fpMagic   = "DEFP"
	fpVersion = 1
)

// SaveFile writes the fingerprint to a .bin file (e.g. "record.mp3.bin").
func SaveFile(fp *Fingerprint, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	if err := Save(fp, w); err != nil {
		return err
	}
	return w.Flush()
}

// Save writes the fingerprint to w in the .bin format.
func Save(fp *Fingerprint, w io.Writer) error {
	bands, frames := fp.NumBands(), fp.NumFrames()

	if _, err := io.WriteString(w, fpMagic); err != nil {
		return err
	}
	for _, v := range []any{
		uint8(fpVersion),
		int32(fp.SampleRate),
		int32(fp.HopSamples),
		int32(bands),
		int32(frames),
	} {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}

	// Pack the 0/1 bits, 8 per byte.
	var cur byte
	var n int
	for b := 0; b < bands; b++ {
		for _, bit := range fp.Bits[b] {
			if bit != 0 {
				cur |= 1 << (n % 8)
			}
			n++
			if n%8 == 0 {
				if _, err := w.Write([]byte{cur}); err != nil {
					return err
				}
				cur = 0
			}
		}
	}
	if n%8 != 0 { // flush the last partial byte
		if _, err := w.Write([]byte{cur}); err != nil {
			return err
		}
	}
	return nil
}

// LoadFile reads a fingerprint back from a .bin file — no decoding, no math.
func LoadFile(path string) (*Fingerprint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(bufio.NewReader(f))
}

// Load reads a fingerprint from r in the .bin format.
func Load(r io.Reader) (*Fingerprint, error) {
	magic := make([]byte, 4)
	if _, err := io.ReadFull(r, magic); err != nil {
		return nil, fmt.Errorf("fingerprint: reading header: %w", err)
	}
	if string(magic) != fpMagic {
		return nil, fmt.Errorf("fingerprint: not a %s file", fpMagic)
	}

	var version uint8
	var sampleRate, hopSamples, bands, frames int32
	for _, v := range []any{&version, &sampleRate, &hopSamples, &bands, &frames} {
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	if version != fpVersion {
		return nil, fmt.Errorf("fingerprint: unsupported version %d", version)
	}

	total := int(bands) * int(frames)
	packed := make([]byte, (total+7)/8)
	if _, err := io.ReadFull(r, packed); err != nil {
		return nil, fmt.Errorf("fingerprint: reading bits: %w", err)
	}

	bits := make([][]int8, bands)
	n := 0
	for b := int32(0); b < bands; b++ {
		row := make([]int8, frames)
		for f := int32(0); f < frames; f++ {
			if packed[n/8]&(1<<(n%8)) != 0 {
				row[f] = 1
			}
			n++
		}
		bits[b] = row
	}

	return &Fingerprint{
		Bits:       bits,
		SampleRate: int(sampleRate),
		HopSamples: int(hopSamples),
	}, nil
}
