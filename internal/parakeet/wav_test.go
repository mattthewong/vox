package parakeet

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// buildWAV constructs a minimal 16-bit mono PCM WAV around the given samples.
func buildWAV(t *testing.T, sampleRate int, samples []int16) []byte {
	t.Helper()
	var b bytes.Buffer
	dataLen := len(samples) * 2

	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")

	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1)) // mono
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate*2))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))

	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataLen))
	for _, s := range samples {
		binary.Write(&b, binary.LittleEndian, s)
	}
	return b.Bytes()
}

func TestDecodeWAVBasic(t *testing.T) {
	wav := buildWAV(t, 16000, []int16{0, 16384, -16384, 32767})
	got, rate, err := DecodeWAV(wav)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	if rate != 16000 {
		t.Errorf("rate = %d, want 16000", rate)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	want := []float32{0, 0.5, -0.5, 32767.0 / 32768.0}
	for i := range want {
		if math.Abs(float64(got[i]-want[i])) > 1e-6 {
			t.Errorf("sample %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestDecodeWAVSkipsUnknownChunks(t *testing.T) {
	base := buildWAV(t, 16000, []int16{100, 200})
	// Splice a LIST chunk between "WAVE" and "fmt ".
	var b bytes.Buffer
	b.Write(base[:12])
	b.WriteString("LIST")
	binary.Write(&b, binary.LittleEndian, uint32(4))
	b.WriteString("INFO")
	b.Write(base[12:])
	// Fix up the RIFF size field.
	out := b.Bytes()
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))

	got, _, err := DecodeWAV(out)
	if err != nil {
		t.Fatalf("DecodeWAV: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestDecodeWAVErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"too short", []byte("RIFF")},
		{"not riff", append([]byte("XXXXWAVE"), make([]byte, 40)...)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := DecodeWAV(tt.data); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDecodeWAVRejectsNonPCM(t *testing.T) {
	wav := buildWAV(t, 16000, []int16{1, 2})
	// Flip the audio format field (offset 20) from 1 (PCM) to 3 (float).
	binary.LittleEndian.PutUint16(wav[20:22], 3)
	if _, _, err := DecodeWAV(wav); err == nil {
		t.Error("expected error for non-PCM format")
	}
}

func TestDuration(t *testing.T) {
	// 16000 samples at 16 kHz is exactly one second.
	wav := buildWAV(t, 16000, make([]int16, 16000))
	got, err := Duration(wav)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if got != time.Second {
		t.Errorf("Duration = %v, want 1s", got)
	}
}

func TestDurationIgnoresExtraChunks(t *testing.T) {
	// This is the case a fixed 44-byte-header parser gets wrong: a LIST
	// chunk shifts the data offset, so byte-arithmetic overcounts the audio.
	base := buildWAV(t, 16000, make([]int16, 8000)) // 0.5s
	var b bytes.Buffer
	b.Write(base[:12])
	b.WriteString("LIST")
	binary.Write(&b, binary.LittleEndian, uint32(4))
	b.WriteString("INFO")
	b.Write(base[12:])
	out := b.Bytes()
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))

	got, err := Duration(out)
	if err != nil {
		t.Fatalf("Duration: %v", err)
	}
	if got != 500*time.Millisecond {
		t.Errorf("Duration = %v, want 500ms", got)
	}
}

func TestDurationRejectsInvalid(t *testing.T) {
	if _, err := Duration([]byte("nope")); err == nil {
		t.Error("expected error for invalid WAV")
	}
}
