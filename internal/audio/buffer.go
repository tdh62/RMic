package audio

import (
	"encoding/binary"
	"math"
	"sync"
)

const SampleRate = 48000
const Channels = 2

// Buffer is a bounded interleaved PCM queue. Old samples are dropped on overflow
// so a slow output device cannot cause latency to grow indefinitely.
type Buffer struct {
	channels                    int
	mu                          sync.Mutex
	data                        []int16
	head, size                  int
	primed                      bool
	prebuffer                   int
	written, dropped, underruns uint64
	level                       float64
}

type Stats struct {
	BufferedMS float64 `json:"buffered_ms"`
	Samples    uint64  `json:"samples"`
	Dropped    uint64  `json:"dropped_samples"`
	Underruns  uint64  `json:"underruns"`
	Level      float64 `json:"level"`
}

func NewBuffer() *Buffer { return NewBufferWithLatency(40, 200) }
func NewBufferWithLatency(prebufferMS, capacityMS int) *Buffer {
	return NewBufferForChannels(prebufferMS, capacityMS, Channels)
}
func NewBufferForChannels(prebufferMS, capacityMS, channels int) *Buffer {
	if ValidateChannels(channels) != nil {
		panic("unsupported PCM channel count")
	}
	return &Buffer{channels: channels, data: make([]int16, SampleRate*channels*capacityMS/1000), prebuffer: SampleRate * channels * prebufferMS / 1000}
}
func (b *Buffer) Channels() int { return b.channels }

func (b *Buffer) Write(pcm []int16) {
	b.mu.Lock()
	defer b.mu.Unlock()
	pcm = pcm[:len(pcm)-len(pcm)%b.channels]
	b.written += uint64(len(pcm))
	for _, s := range pcm {
		if b.size == len(b.data) {
			// Evict a complete frame, preserving channel alignment.
			b.head = (b.head + b.channels) % len(b.data)
			b.size -= b.channels
			b.dropped += uint64(b.channels)
		}
		b.data[(b.head+b.size)%len(b.data)] = s
		b.size++
	}
}

func (b *Buffer) Read(out []byte, gain float64, muted bool) {
	clear(out)
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.primed {
		if b.size < b.prebuffer {
			b.level = 0
			return
		}
		b.primed = true
	}
	count := min(len(out)/2, b.size)
	count -= count % b.channels
	var sum float64
	for i := 0; i < count; i++ {
		v := float64(b.data[(b.head+i)%len(b.data)]) * gain
		if muted {
			v = 0
		}
		v = max(-32768, min(32767, v))
		sum += v * v
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(v)))
	}
	b.head = (b.head + count) % len(b.data)
	b.size -= count
	if count < len(out)/2 {
		b.underruns++
		b.primed = false
	}
	b.level = 0
	if count > 0 {
		b.level = math.Sqrt(sum/float64(count)) / 32768
	}
}

func (b *Buffer) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Stats{float64(b.size) * 1000 / float64(SampleRate*b.channels), b.written, b.dropped, b.underruns, b.level}
}
