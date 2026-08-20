package parakeet

import (
	"encoding/binary"
	"fmt"
	"time"
)

// DecodeWAV parses a 16-bit PCM mono WAV payload into float32 samples in the
// range [-1, 1], along with the declared sample rate. It walks the RIFF chunk
// list rather than assuming a fixed 44-byte header, because sox and ffmpeg
// both emit optional metadata chunks (LIST, JUNK) and a fixed offset silently
// misreads those files.
//
// Exported because cmd/voxbench needs the same parsing to compute clip
// duration. Duplicating a second, simpler parser there reintroduces exactly
// the fixed-header bug this one avoids.
func DecodeWAV(b []byte) ([]float32, int, error) {
	if len(b) < 12 {
		return nil, 0, fmt.Errorf("wav too short (%d bytes)", len(b))
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("not a RIFF/WAVE payload")
	}

	var (
		sampleRate    int
		channels      int
		bitsPerSample int
		sawFmt        bool
	)

	off := 12
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		if size < 0 || body+size > len(b) {
			size = len(b) - body // tolerate a truncated final chunk
		}

		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, fmt.Errorf("fmt chunk too small (%d)", size)
			}
			format := binary.LittleEndian.Uint16(b[body : body+2])
			if format != 1 {
				return nil, 0, fmt.Errorf("unsupported wav format %d (want 1 = PCM)", format)
			}
			channels = int(binary.LittleEndian.Uint16(b[body+2 : body+4]))
			sampleRate = int(binary.LittleEndian.Uint32(b[body+4 : body+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(b[body+14 : body+16]))
			sawFmt = true

		case "data":
			if !sawFmt {
				return nil, 0, fmt.Errorf("data chunk before fmt chunk")
			}
			if bitsPerSample != 16 {
				return nil, 0, fmt.Errorf("unsupported bit depth %d (want 16)", bitsPerSample)
			}
			if channels != 1 {
				return nil, 0, fmt.Errorf("unsupported channel count %d (want 1)", channels)
			}
			pcm := b[body : body+size]
			out := make([]float32, len(pcm)/2)
			for i := range out {
				s := int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
				out[i] = float32(s) / 32768.0
			}
			return out, sampleRate, nil
		}

		off = body + size
		if size%2 == 1 {
			off++ // RIFF chunks are word-aligned
		}
	}
	return nil, 0, fmt.Errorf("no data chunk found")
}

// Duration returns the playable length of a WAV payload, derived from the
// decoded sample count and rate rather than from byte offsets.
func Duration(b []byte) (time.Duration, error) {
	samples, rate, err := DecodeWAV(b)
	if err != nil {
		return 0, err
	}
	if rate <= 0 {
		return 0, fmt.Errorf("invalid sample rate %d", rate)
	}
	return time.Duration(float64(len(samples)) / float64(rate) * float64(time.Second)), nil
}
