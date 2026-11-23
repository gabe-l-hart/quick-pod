// Package audio parses and concatenates PCM WAV audio.
package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type wavInfo struct {
	formatTag   uint16
	channels    uint16
	sampleRate  uint32
	bitsPerSample uint16
	data        []byte
}

// ParseWAV parses a WAV file from memory.
func ParseWAV(raw []byte) (*wavInfo, error) {
	if len(raw) < 44 {
		return nil, fmt.Errorf("wav: file too small (%d bytes)", len(raw))
	}
	if string(raw[0:4]) != "RIFF" {
		return nil, fmt.Errorf("wav: not a RIFF file")
	}
	if string(raw[8:12]) != "WAVE" {
		return nil, fmt.Errorf("wav: not a WAVE file")
	}
	// walk chunks
	data := []byte{}
	var info *wavInfo
	for offset := 12; offset < len(raw)-8; {
		chunkID := string(raw[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		offset += 8
		if offset+size > len(raw) {
			return nil, fmt.Errorf("wav: chunk %s overruns file", chunkID)
		}
		switch chunkID {
		case "fmt ":
			if size < 16 {
				return nil, fmt.Errorf("wav: fmt chunk too small")
			}
			formatTag := binary.LittleEndian.Uint16(raw[offset:])
			channels := binary.LittleEndian.Uint16(raw[offset+2:])
			sampleRate := binary.LittleEndian.Uint32(raw[offset+4:])
			bitsPerSample := binary.LittleEndian.Uint16(raw[offset+14:])
			if info == nil {
				info = &wavInfo{}
			}
			info.formatTag = formatTag
			info.channels = channels
			info.sampleRate = sampleRate
			info.bitsPerSample = bitsPerSample
		case "data":
			data = raw[offset : offset+size]
		}
		offset += size
		if size%2 == 1 {
			offset++
		}
	}
	if info == nil {
		return nil, fmt.Errorf("wav: no fmt chunk")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("wav: no data chunk")
	}
	info.data = data
	return info, nil
}

// WriteWAV writes a WAV file with the given PCM data and format.
func WriteWAV(path string, info *wavInfo) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeWAV(f, info)
}

func writeWAV(w io.Writer, info *wavInfo) error {
	dataLen := len(info.data)
	byteRate := info.sampleRate * uint32(info.channels) * uint32(info.bitsPerSample/8)
	blockAlign := info.channels * (info.bitsPerSample / 8)
	var buf bytes.Buffer
	buf.Write([]byte("RIFF"))
	binary.Write(&buf, binary.LittleEndian, uint32(36+dataLen))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, info.formatTag)
	binary.Write(&buf, binary.LittleEndian, info.channels)
	binary.Write(&buf, binary.LittleEndian, info.sampleRate)
	binary.Write(&buf, binary.LittleEndian, byteRate)
	binary.Write(&buf, binary.LittleEndian, blockAlign)
	binary.Write(&buf, binary.LittleEndian, info.bitsPerSample)
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(dataLen))
	if _, err := w.Write(buf.Bytes()); err != nil {
		return err
	}
	_, err := w.Write(info.data)
	return err
}

// Concatenate merges WAV byte slices into one WAV. Formats must match.
func Concatenate(parts [][]byte) ([]byte, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("audio: no WAV parts")
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	infos := make([]*wavInfo, len(parts))
	for i, p := range parts {
		info, err := ParseWAV(p)
		if err != nil {
			return nil, fmt.Errorf("audio: part %d: %w", i, err)
		}
		infos[i] = info
	}
	ref := infos[0]
	var total bytes.Buffer
	total.Grow(len(parts) * 44000)
	for _, info := range infos {
		if info.formatTag != ref.formatTag || info.channels != ref.channels || info.sampleRate != ref.sampleRate || info.bitsPerSample != ref.bitsPerSample {
			return nil, fmt.Errorf("audio: mismatched WAV format (%s vs %s)",
				fmt.Sprintf("tag=%d ch=%d sr=%d bp=%d", ref.formatTag, ref.channels, ref.sampleRate, ref.bitsPerSample),
				fmt.Sprintf("tag=%d ch=%d sr=%d bp=%d", info.formatTag, info.channels, info.sampleRate, info.bitsPerSample))
		}
		total.Write(info.data)
	}
	merged := &wavInfo{
		formatTag:    ref.formatTag,
		channels:     ref.channels,
		sampleRate:   ref.sampleRate,
		bitsPerSample: ref.bitsPerSample,
		data:         total.Bytes(),
	}
	var out bytes.Buffer
	if err := writeWAV(&out, merged); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// MergeToFile writes the merged WAV to a path.
func MergeToFile(path string, parts [][]byte) error {
	merged, err := Concatenate(parts)
	if err != nil {
		return err
	}
	return os.WriteFile(path, merged, 0o644)
}
