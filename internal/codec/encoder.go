package codec

/*
#cgo pkg-config: opus
#include <opus.h>
static int rmic_setup_encoder(OpusEncoder *e, int bitrate, int channels) {
    int result = opus_encoder_ctl(e, OPUS_SET_BITRATE(bitrate));
    if (result != OPUS_OK) return result;
    result = opus_encoder_ctl(e, OPUS_SET_FORCE_CHANNELS(channels));
    if (result != OPUS_OK) return result;
    return opus_encoder_ctl(e, OPUS_SET_DTX(0));
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const FrameSamples = 960 * 2 // 20 ms, 48 kHz stereo.

type Encoder struct {
	ptr          *C.OpusEncoder
	frameSamples int
}

func NewEncoder() (*Encoder, error) { return NewEncoderWithConfig(96000, 2, 20) }
func NewEncoderWithConfig(bitrate, channels, frameMS int) (*Encoder, error) {
	if bitrate < 6000 || bitrate > 510000 || (channels != 1 && channels != 2) || (frameMS != 20 && frameMS != 40) {
		return nil, fmt.Errorf("invalid Opus encoder configuration")
	}
	var code C.int
	ptr := C.opus_encoder_create(48000, 2, C.OPUS_APPLICATION_AUDIO, &code)
	if code != C.OPUS_OK {
		return nil, fmt.Errorf("opus encoder: %s", C.GoString(C.opus_strerror(code)))
	}
	if code = C.rmic_setup_encoder(ptr, C.int(bitrate), C.int(channels)); code != C.OPUS_OK {
		C.opus_encoder_destroy(ptr)
		return nil, fmt.Errorf("configure opus encoder: %s", C.GoString(C.opus_strerror(code)))
	}
	return &Encoder{ptr: ptr, frameSamples: 48000 * 2 * frameMS / 1000}, nil
}
func (e *Encoder) Encode(pcm []int16, packet []byte) (int, error) {
	if e.ptr == nil || len(pcm) != e.frameSamples || len(packet) < 1276 {
		return 0, fmt.Errorf("invalid Opus encode input")
	}
	n := C.opus_encode(e.ptr, (*C.opus_int16)(unsafe.Pointer(&pcm[0])), C.int(e.frameSamples/2), (*C.uchar)(unsafe.Pointer(&packet[0])), C.opus_int32(len(packet)))
	if n < 0 {
		return 0, fmt.Errorf("encode opus: %s", C.GoString(C.opus_strerror(n)))
	}
	return int(n), nil
}
func (e *Encoder) Close() {
	if e.ptr != nil {
		C.opus_encoder_destroy(e.ptr)
		e.ptr = nil
	}
}
