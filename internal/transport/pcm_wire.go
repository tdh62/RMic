package transport

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

const pcmHeader = 24

// Each compressed payload is an independent zlib stream, with an exact decoded
// size limit. Raw fallback avoids expanding incompressible PCM.
type pcmEncoder struct {
	compressed bool
	result     bytes.Buffer
	writer     *zlib.Writer
}

func newPCMEncoder(compressed bool) *pcmEncoder {
	encoder := &pcmEncoder{compressed: compressed}
	if compressed {
		encoder.writer, _ = zlib.NewWriterLevel(&encoder.result, zlib.BestSpeed)
	}
	return encoder
}
func encodePCM(raw []byte, channels int, offset uint64, compressed bool) ([]byte, error) {
	return newPCMEncoder(compressed).encode(raw, channels, offset)
}
func (e *pcmEncoder) encode(raw []byte, channels int, offset uint64) ([]byte, error) {
	payload := raw
	flags := uint32(0)
	if e.compressed {
		e.result.Reset()
		e.writer.Reset(&e.result)
		if _, err := e.writer.Write(raw); err != nil {
			return nil, err
		}
		if err := e.writer.Close(); err != nil {
			return nil, err
		}
		if e.result.Len() < len(raw) {
			payload = e.result.Bytes()
			flags = 1
		}
	}
	packet := make([]byte, pcmHeader+len(payload))
	copy(packet, "RMPC")
	binary.LittleEndian.PutUint16(packet[4:], 2)
	binary.LittleEndian.PutUint16(packet[6:], uint16(channels))
	binary.LittleEndian.PutUint64(packet[8:], offset)
	binary.LittleEndian.PutUint32(packet[16:], 960)
	binary.LittleEndian.PutUint32(packet[20:], flags)
	copy(packet[pcmHeader:], payload)
	return packet, nil
}
func decodePCM(packet []byte, channels int, offset uint64, compressed bool) ([]byte, error) {
	size := 960 * channels * 2
	if len(packet) < pcmHeader || len(packet) > pcmHeader+size || string(packet[:4]) != "RMPC" || binary.LittleEndian.Uint16(packet[4:]) != 2 || int(binary.LittleEndian.Uint16(packet[6:])) != channels || binary.LittleEndian.Uint64(packet[8:]) != offset || binary.LittleEndian.Uint32(packet[16:]) != 960 {
		return nil, fmt.Errorf("invalid PCM frame")
	}
	flags := binary.LittleEndian.Uint32(packet[20:])
	raw := packet[pcmHeader:]
	if flags == 1 && compressed {
		source := bytes.NewReader(raw)
		reader, err := zlib.NewReader(source)
		if err != nil {
			return nil, err
		}
		decoded, err := io.ReadAll(io.LimitReader(reader, int64(size+1)))
		reader.Close()
		if err != nil || source.Len() != 0 {
			return nil, fmt.Errorf("invalid PCM compressed stream")
		}
		raw = decoded
	} else if flags != 0 {
		return nil, fmt.Errorf("invalid PCM flags")
	}
	if len(raw) != size {
		return nil, fmt.Errorf("invalid PCM decoded length")
	}
	return raw, nil
}
