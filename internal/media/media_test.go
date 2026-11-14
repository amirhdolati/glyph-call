package media

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/amirhdolati/glyph-call/internal/session"
)

func TestVideoModesAndWirePrivacyBoundary(t *testing.T) {
	src := Demo(0)
	for _, p := range []session.Video{{Mode: "hidden", Style: "ascii", Width: 32, Height: 16, FPS: 8}, {Mode: "fun", Style: "ascii", Color: true, Width: 80, Height: 30, FPS: 12}, {Mode: "fun", Style: "unicode", Color: true, Width: 80, Height: 30, FPS: 12}, {Mode: "graphic", Style: "image", Width: 640, Height: 360, FPS: 12}} {
		frame, err := MakeFrame(src, p)
		if err != nil {
			t.Fatal(err)
		}
		data, err := frame.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		received, err := ParseFrame(data, p)
		if err != nil {
			t.Fatal(err)
		}
		if received.Mode != p.Mode || received.Width != frame.Width {
			t.Fatal("frame changed")
		}
		if p.Mode == "graphic" {
			png, err := received.PNG()
			if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
				t.Fatal("real image failed")
			}
			hidden := session.Video{Mode: "hidden", Style: "ascii", Width: 32, Height: 16}
			if _, err := ParseFrame(data, hidden); err == nil {
				t.Fatal("real image accepted in hidden mode")
			}
		}
		if p.Mode == "hidden" {
			if len(data) != 8+frame.Width*frame.Height || bytes.Contains(data, []byte{0xff, 0xd8}) {
				t.Fatal("hidden wire contains more than ASCII cells")
			}
		}
	}
}
func TestUnsafeCharacterAndOversizedFrameRejected(t *testing.T) {
	p := session.Video{Mode: "hidden", Style: "ascii", Width: 32, Height: 16}
	f, _ := MakeFrame(Demo(0), p)
	data, _ := f.Marshal()
	data[8] = 27
	if _, err := ParseFrame(data, p); err == nil {
		t.Fatal("terminal escape accepted")
	}
	binary.BigEndian.PutUint16(data[4:], 65535)
	if _, err := ParseFrame(data, p); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
func TestChunkReorderingLossAndBoundedReassembly(t *testing.T) {
	data := make([]byte, ChunkSize*3+17)
	copy(data, "test frame")
	parts := Chunks(data, 1)
	var a Assembler
	for i := len(parts) - 1; i >= 1; i-- {
		if a.Push(parts[i]) != nil {
			t.Fatal("incomplete frame completed")
		}
	}
	next := Chunks(data, 2)
	var result []byte
	for i := len(next) - 1; i >= 0; i-- {
		if r := a.Push(next[i]); r != nil {
			result = r
		}
	}
	if !bytes.Equal(result, data) {
		t.Fatal("reordered frame failed")
	}
	if a.Push(parts[0]) != nil || a.Push(next[0]) != nil {
		t.Fatal("stale/replayed frame accepted")
	}
	bad := append([]byte(nil), next[0]...)
	binary.BigEndian.PutUint32(bad[8:], MaxFrame+1)
	if a.Push(bad) != nil {
		t.Fatal("oversized reassembly accepted")
	}
}
func TestNaturalVoiceAndEffectsAcrossAllQualities(t *testing.T) {
	input := make([]int16, DeviceSamples)
	for i := range input {
		input[i] = int16(12000 * math.Sin(2*math.Pi*440*float64(i)/DeviceRate))
	}
	for _, rate := range []int{8000, 16000, 24000, 48000} {
		for _, mode := range []string{"real", "robot", "8bit", "radio"} {
			v := Voice{Mode: mode}
			pcm := v.Encode(input, rate)
			if len(pcm) != rate/50*2 {
				t.Fatal("wrong packet duration")
			}
			if mode == "real" && rate == 48000 {
				for i, sample := range input {
					actual := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
					if math.Abs(float64(actual-sample)) > 2 {
						t.Fatal("real voice changed")
					}
				}
			}
			if mode == "robot" && rate == 48000 {
				real := (&Voice{Mode: "real"}).Encode(input, rate)
				if bytes.Equal(real, pcm) {
					t.Fatal("robot effect missing")
				}
			}
		}
	}
}
func TestAudioJitterBufferReordersAndFillsLoss(t *testing.T) {
	b := NewAudioBuffer(16000)
	for _, seq := range []uint32{2, 0, 1} {
		pcm := make([]byte, 640)
		for i := 0; i < 320; i++ {
			binary.LittleEndian.PutUint16(pcm[i*2:], uint16(seq+1))
		}
		b.Push(AudioPacket(seq, pcm))
	}
	for expected := int16(1); expected <= 3; expected++ {
		out := b.Read()
		if len(out) != 960 || out[0] != expected {
			t.Fatal("audio ordering failed")
		}
	}
	if out := b.Read(); out[0] != 0 {
		t.Fatal("lost packet was not silence")
	}
}
