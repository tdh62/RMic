package ui

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/image/font/sfnt"
)

// standaloneFont extracts the first face of an installed TTC into an in-memory
// SFNT. Fyne's font parser accepts individual faces, not collections. Tables in
// a TTC use collection-relative offsets and may be shared by multiple faces.
func standaloneFont(data []byte) ([]byte, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("font header truncated")
	}
	if string(data[:4]) != "ttcf" {
		if _, err := sfnt.Parse(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	if len(data) < 16 || binary.BigEndian.Uint32(data[8:12]) == 0 {
		return nil, fmt.Errorf("empty font collection")
	}
	offset := uint64(binary.BigEndian.Uint32(data[12:16]))
	if offset+12 > uint64(len(data)) {
		return nil, fmt.Errorf("invalid face offset")
	}
	count := uint64(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
	directorySize := uint64(12) + count*16
	if offset+directorySize > uint64(len(data)) {
		return nil, fmt.Errorf("font directory truncated")
	}
	out := append([]byte(nil), data[offset:offset+directorySize]...)
	headOffset := -1
	for i := uint64(0); i < count; i++ {
		record := 12 + i*16
		original := data[offset+record : offset+record+16]
		start := uint64(binary.BigEndian.Uint32(original[8:12]))
		length := uint64(binary.BigEndian.Uint32(original[12:16]))
		if start+length > uint64(len(data)) {
			return nil, fmt.Errorf("font table truncated")
		}
		if uint64(len(out))+length > 128<<20 {
			return nil, fmt.Errorf("font exceeds size limit")
		}
		newOffset := len(out)
		out = append(out, data[start:start+length]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
		binary.BigEndian.PutUint32(out[record+8:record+12], uint32(newOffset))
		if string(original[:4]) == "head" && length >= 12 {
			headOffset = newOffset
			clear(out[newOffset+8 : newOffset+12])
		}
	}
	if headOffset >= 0 {
		var sum uint32
		for i := 0; i < len(out); i += 4 {
			sum += binary.BigEndian.Uint32(out[i : i+4])
		}
		binary.BigEndian.PutUint32(out[headOffset+8:headOffset+12], 0xB1B0AFBA-sum)
	}
	if _, err := sfnt.Parse(out); err != nil {
		return nil, err
	}
	return out, nil
}
