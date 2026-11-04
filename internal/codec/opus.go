// Package codec owns the small libopus boundary used by both audio directions.
package codec

/*
#cgo pkg-config: opus
#include <opus.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

type Decoder struct{ ptr *C.OpusDecoder }

func NewDecoder() (*Decoder, error) {
	var code C.int
	ptr := C.opus_decoder_create(48000, 2, &code)
	if code != C.OPUS_OK {
		return nil, fmt.Errorf("opus decoder: %s", C.GoString(C.opus_strerror(code)))
	}
	return &Decoder{ptr}, nil
}

// Decode returns interleaved stereo samples at 48 kHz. A maximum Opus packet
// spans 120 ms, so callers must supply at least 11520 sample slots.
func (d *Decoder) Decode(packet []byte, pcm []int16) (int, error) {
	if d.ptr == nil || len(packet) == 0 || len(pcm) < 11520 {
		return 0, fmt.Errorf("invalid Opus decode input")
	}
	n := C.opus_decode(d.ptr, (*C.uchar)(unsafe.Pointer(&packet[0])), C.opus_int32(len(packet)), (*C.opus_int16)(unsafe.Pointer(&pcm[0])), C.int(len(pcm)/2), 0)
	if n < 0 {
		return 0, fmt.Errorf("decode opus: %s", C.GoString(C.opus_strerror(n)))
	}
	return int(n) * 2, nil
}
func (d *Decoder) Close() {
	if d.ptr != nil {
		C.opus_decoder_destroy(d.ptr)
		d.ptr = nil
	}
}
