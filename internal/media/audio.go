package media

import (
	"encoding/binary"
	"math"
	"sync"
)

const DeviceRate = 48000
const DeviceSamples = 960

type Voice struct {
	Mode                string
	phase               float64
	low, previous, high float64
}

func (v *Voice) Encode(input []int16, rate int) []byte {
	samples := Resample(input, DeviceRate, rate)
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		x := float64(s) / 32768
		switch v.Mode {
		case "robot":
			x = math.Tanh(2.2*x) * math.Sin(v.phase)
			v.phase = math.Mod(v.phase+2*math.Pi*83/float64(rate), 2*math.Pi)
		case "8bit":
			x = math.Round(x*127) / 127
		case "radio":
			alpha := 1 / (1 + 2*math.Pi*300/float64(rate))
			v.high = alpha * (v.high + x - v.previous)
			v.previous = x
			beta := 1 - math.Exp(-2*math.Pi*3000/float64(rate))
			v.low += beta * (v.high - v.low)
			x = math.Tanh(2*v.low) * 0.8
		case "off":
			x = 0
		}
		x = max(-1, min(1, x))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(math.Round(x*32767))))
	}
	return out
}

func Resample(input []int16, from, to int) []int16 {
	if from == to {
		return append([]int16(nil), input...)
	}
	n := len(input) * to / from
	out := make([]int16, n)
	if len(input) == 0 {
		return out
	}
	// Box-average while reducing rate to attenuate aliasing; linear upsampling.
	for i := range out {
		if to < from {
			start := i * from / to
			end := min(len(input), (i+1)*from/to)
			sum := int64(0)
			for j := start; j < end; j++ {
				sum += int64(input[j])
			}
			out[i] = int16(sum / int64(max(1, end-start)))
		} else {
			position := float64(i) * float64(from) / float64(to)
			lo := min(int(position), len(input)-1)
			hi := min(lo+1, len(input)-1)
			f := position - float64(lo)
			out[i] = int16(float64(input[lo])*(1-f) + float64(input[hi])*f)
		}
	}
	return out
}

func AudioPacket(sequence uint32, pcm []byte) []byte {
	out := make([]byte, 6+len(pcm))
	copy(out, "A1")
	binary.BigEndian.PutUint32(out[2:], sequence)
	copy(out[6:], pcm)
	return out
}

type AudioBuffer struct {
	mu       sync.Mutex
	rate     int
	packets  map[uint32][]byte
	expected uint32
	started  bool
	misses   int
}

func NewAudioBuffer(rate int) *AudioBuffer {
	return &AudioBuffer{rate: rate, packets: map[uint32][]byte{}}
}
func (b *AudioBuffer) Push(packet []byte) {
	if len(packet) != 6+b.rate/50*2 || string(packet[:2]) != "A1" {
		return
	}
	seq := binary.BigEndian.Uint32(packet[2:])
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started && seq < b.expected {
		return
	}
	b.packets[seq] = append([]byte(nil), packet[6:]...)
	if len(b.packets) > 8 {
		oldest := seq
		for s := range b.packets {
			oldest = min(oldest, s)
		}
		delete(b.packets, oldest)
		b.expected = oldest + 1
	}
}
func (b *AudioBuffer) Read() []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started {
		if len(b.packets) < 3 {
			return make([]int16, DeviceSamples)
		}
		b.expected = math.MaxUint32
		for s := range b.packets {
			b.expected = min(b.expected, s)
		}
		b.started = true
	}
	packet := b.packets[b.expected]
	delete(b.packets, b.expected)
	b.expected++
	if packet == nil {
		b.misses++
		if b.misses >= 10 {
			b.started = false
			b.misses = 0
		}
		return make([]int16, DeviceSamples)
	}
	b.misses = 0
	samples := make([]int16, len(packet)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(packet[i*2:]))
	}
	return Resample(samples, b.rate, DeviceRate)
}
