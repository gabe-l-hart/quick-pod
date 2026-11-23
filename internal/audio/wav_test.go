package audio

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func makeWAV(channels, bits uint16, sampleRate uint32, dataLen int) []byte {
	data := make([]byte, dataLen)
	// fill with constant PCM16 samples
	blockAlign := channels * (bits / 8)
	for i := 0; i < dataLen; i += 2 {
		binary.LittleEndian.PutUint16(data[i:], uint16(i%32768))
	}
	info := &wavInfo{formatTag: 1, channels: channels, sampleRate: sampleRate, bitsPerSample: bits, data: data}
	var out bytes.Buffer
	if err := writeWAV(&out, info); err != nil {
		panic(err)
	}
	_ = blockAlign
	return out.Bytes()
}

func TestParseWAV(t *testing.T) {
	raw := makeWAV(1, 16, 8000, 1000)
	info, err := ParseWAV(raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.sampleRate != 8000 || info.channels != 1 || info.bitsPerSample != 16 {
		t.Fatalf("info = %+v", info)
	}
	if len(info.data) != 1000 {
		t.Fatalf("data len = %d", len(info.data))
	}
}

func TestConcatenate(t *testing.T) {
	a := makeWAV(1, 16, 8000, 1000)
	b := makeWAV(1, 16, 8000, 700)
	merged, err := Concatenate([][]byte{a, b})
	if err != nil {
		t.Fatal(err)
	}
	info, err := ParseWAV(merged)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.data) != 1700 {
		t.Fatalf("merged len = %d", len(info.data))
	}
}

func TestConcatenateMismatch(t *testing.T) {
	a := makeWAV(1, 16, 8000, 100)
	b := makeWAV(1, 16, 44100, 100)
	if _, err := Concatenate([][]byte{a, b}); err == nil {
		t.Fatal("expected format mismatch error")
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := ParseWAV([]byte("garbage")); err == nil {
		t.Fatal("expected error")
	}
}

var _ = strings.Contains
