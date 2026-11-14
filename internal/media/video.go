package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"sync"

	"github.com/amirhdolati/glyph-call/internal/session"
)

const MaxFrame = 2 * 1024 * 1024
const ChunkSize = 24000
const Ramp = " .,:;irsXA253hMHGS#9B&@"
const HiddenRamp = " .:+#"

type Cell struct {
	Rune rune
	RGB  uint32
}
type Frame struct {
	Mode          string
	Style         string
	Width, Height int
	Cells         []Cell
	JPEG          []byte
}

func Resize(src image.Image, width, height int) *image.RGBA {
	b := src.Bounds()
	if rgba, ok := src.(*image.RGBA); ok {
		if width == b.Dx() && height == b.Dy() {
			return rgba
		}
		dst := image.NewRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			sy := b.Min.Y + y*b.Dy()/height
			for x := 0; x < width; x++ {
				sx := b.Min.X + x*b.Dx()/width
				source := rgba.PixOffset(sx, sy)
				target := y*dst.Stride + x*4
				copy(dst.Pix[target:target+4], rgba.Pix[source:source+4])
			}
		}
		return dst
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			sx := b.Min.X + x*b.Dx()/width
			sy := b.Min.Y + y*b.Dy()/height
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
func Fit(src image.Image, width, height int) (int, int) {
	b := src.Bounds()
	w := min(width, b.Dx())
	h := max(1, b.Dy()*w/b.Dx())
	if h > height {
		h = height
		w = max(1, b.Dx()*h/b.Dy())
	}
	return w, h
}

func MakeFrame(src image.Image, p session.Video) (Frame, error) {
	if p.Mode == "graphic" {
		w, h := Fit(src, p.Width, p.Height)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, Resize(src, w, h), &jpeg.Options{Quality: 85}); err != nil {
			return Frame{}, err
		}
		if buf.Len() > MaxFrame-8 {
			return Frame{}, errors.New("camera frame too large")
		}
		return Frame{Mode: "graphic", Style: "image", Width: w, Height: h, JPEG: buf.Bytes()}, nil
	}
	width := p.Width
	height := min(p.Height, max(1, src.Bounds().Dy()*width/src.Bounds().Dx()/2))
	if height == p.Height {
		width = min(width, max(1, src.Bounds().Dx()*height*2/src.Bounds().Dy()))
	}
	frame := Frame{Mode: p.Mode, Style: p.Style, Width: width, Height: height, Cells: make([]Cell, width*height)}
	if p.Mode == "hidden" {
		width = min(width, 32)
		height = min(height, 16)
		frame.Width = width
		frame.Height = height
		frame.Style = "ascii"
		frame.Cells = make([]Cell, width*height)
	}
	if frame.Style == "unicode" {
		img := Resize(src, width*2, height*4)
		bits := [4][2]uint8{{0, 3}, {1, 4}, {2, 5}, {6, 7}}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				mask := uint8(0)
				r, g, b := 0, 0, 0
				for dy := 0; dy < 4; dy++ {
					for dx := 0; dx < 2; dx++ {
						c := img.RGBAAt(x*2+dx, y*4+dy)
						r += int(c.R)
						g += int(c.G)
						b += int(c.B)
						luma := (int(c.R)*77 + int(c.G)*150 + int(c.B)*29) / 256
						if luma > int([]uint8{40, 160, 100, 220, 130, 70, 200, 110}[dy*2+dx]) {
							mask |= 1 << bits[dy][dx]
						}
					}
				}
				frame.Cells[y*width+x] = Cell{rune(0x2800) + rune(mask), uint32(r/8)<<16 | uint32(g/8)<<8 | uint32(b/8)}
			}
		}
	} else {
		// Area averages for hidden mode remove high-frequency details before encoding.
		b := src.Bounds()
		ramp := Ramp
		if p.Mode == "hidden" {
			ramp = HiddenRamp
		}
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				r, g, blue, count := 0, 0, 0, 0
				step := 1
				if p.Mode != "hidden" {
					step = max(1, b.Dx()/width/2)
				}
				for sy := y * b.Dy() / height; sy < max(y*b.Dy()/height+1, (y+1)*b.Dy()/height); sy += step {
					for sx := x * b.Dx() / width; sx < max(x*b.Dx()/width+1, (x+1)*b.Dx()/width); sx += step {
						cr, cg, cb, _ := src.At(b.Min.X+sx, b.Min.Y+sy).RGBA()
						r += int(cr >> 8)
						g += int(cg >> 8)
						blue += int(cb >> 8)
						count++
					}
				}
				r /= max(1, count)
				g /= max(1, count)
				blue /= max(1, count)
				luma := (r*77 + g*150 + blue*29) / 256
				frame.Cells[y*width+x] = Cell{rune(ramp[luma*(len(ramp)-1)/255]), uint32(r)<<16 | uint32(g)<<8 | uint32(blue)}
			}
		}
	}
	if !p.Color {
		for i := range frame.Cells {
			frame.Cells[i].RGB = 0xffffff
		}
	}
	return frame, nil
}

func (f Frame) Marshal() ([]byte, error) {
	kind := byte(0)
	switch {
	case f.Mode == "graphic":
		kind = 3
	case f.Mode == "fun" && f.Style == "unicode":
		kind = 2
	case f.Mode == "fun":
		kind = 1
	case f.Mode == "hidden":
		kind = 0
	default:
		return nil, errors.New("invalid frame mode")
	}
	out := make([]byte, 8)
	copy(out, "VF1")
	out[3] = kind
	binary.BigEndian.PutUint16(out[4:], uint16(f.Width))
	binary.BigEndian.PutUint16(out[6:], uint16(f.Height))
	if kind == 3 {
		out = append(out, f.JPEG...)
	} else {
		for _, c := range f.Cells {
			if kind == 0 {
				out = append(out, byte(c.Rune))
			} else {
				out = binary.BigEndian.AppendUint16(out, uint16(c.Rune))
				out = append(out, byte(c.RGB>>16), byte(c.RGB>>8), byte(c.RGB))
			}
		}
	}
	if len(out) > MaxFrame {
		return nil, errors.New("frame exceeds size limit")
	}
	return out, nil
}

func ParseFrame(data []byte, allowed session.Video) (Frame, error) {
	if len(data) < 8 || len(data) > MaxFrame || string(data[:3]) != "VF1" {
		return Frame{}, errors.New("invalid frame")
	}
	kind := data[3]
	w := int(binary.BigEndian.Uint16(data[4:]))
	h := int(binary.BigEndian.Uint16(data[6:]))
	if w < 1 || h < 1 || w > allowed.Width || h > allowed.Height {
		return Frame{}, errors.New("frame exceeds negotiated dimensions")
	}
	f := Frame{Width: w, Height: h, Mode: "hidden", Style: "ascii"}
	switch kind {
	case 0:
		if allowed.Mode != "hidden" {
			return f, errors.New("unexpected hidden frame")
		}
	case 1, 2:
		if allowed.Mode != "fun" {
			return f, errors.New("unexpected fun frame")
		}
		f.Mode = "fun"
		if kind == 2 {
			if allowed.Style != "unicode" {
				return f, errors.New("unicode not negotiated")
			}
			f.Style = "unicode"
		}
	case 3:
		if allowed.Mode != "graphic" {
			return f, errors.New("graphic frame forbidden by negotiated mode")
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data[8:]))
		if err != nil || cfg.Width != w || cfg.Height != h {
			return f, errors.New("invalid image dimensions")
		}
		f.Mode = "graphic"
		f.Style = "image"
		f.JPEG = append([]byte(nil), data[8:]...)
		return f, nil
	default:
		return f, errors.New("unknown frame kind")
	}
	unit := 5
	if kind == 0 {
		unit = 1
		if w > 32 || h > 16 {
			return f, errors.New("hidden resolution limit")
		}
	}
	if w > 120 || h > 48 || len(data) != 8+w*h*unit {
		return f, errors.New("invalid character frame length")
	}
	f.Cells = make([]Cell, w*h)
	for i := range f.Cells {
		offset := 8 + i*unit
		c := Cell{RGB: 0xffffff}
		if kind == 0 {
			c.Rune = rune(data[offset])
			if !strings.ContainsRune(HiddenRamp, c.Rune) {
				return f, errors.New("unsafe hidden character")
			}
		} else {
			c.Rune = rune(binary.BigEndian.Uint16(data[offset:]))
			c.RGB = uint32(data[offset+2])<<16 | uint32(data[offset+3])<<8 | uint32(data[offset+4])
			if kind == 1 && !strings.ContainsRune(Ramp, c.Rune) || kind == 2 && (c.Rune < 0x2800 || c.Rune > 0x28ff) {
				return f, errors.New("unsafe terminal character")
			}
		}
		f.Cells[i] = c
	}
	return f, nil
}

func (f Frame) PNG() ([]byte, error) {
	if f.Mode != "graphic" {
		return nil, errors.New("not a graphic frame")
	}
	img, err := jpeg.Decode(bytes.NewReader(f.JPEG))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	err = enc.Encode(&buf, img)
	return buf.Bytes(), err
}
func Demo(t int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 640; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8((x + t) % 256), uint8(y % 256), uint8((x/2 + y + t) % 256), 255})
		}
	}
	return img
}

func Chunks(data []byte, seq uint32) [][]byte {
	count := (len(data) + ChunkSize - 1) / ChunkSize
	out := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		end := min(len(data), (i+1)*ChunkSize)
		p := make([]byte, 16+end-i*ChunkSize)
		copy(p, "VCG1")
		binary.BigEndian.PutUint32(p[4:], seq)
		binary.BigEndian.PutUint32(p[8:], uint32(len(data)))
		binary.BigEndian.PutUint16(p[12:], uint16(i))
		binary.BigEndian.PutUint16(p[14:], uint16(count))
		copy(p[16:], data[i*ChunkSize:end])
		out = append(out, p)
	}
	return out
}

type Assembler struct {
	mu            sync.Mutex
	seq           uint32
	started, done bool
	total, count  int
	parts         map[int][]byte
}

func (a *Assembler) Push(p []byte) []byte {
	if len(p) <= 16 || len(p) > 16+ChunkSize || string(p[:4]) != "VCG1" {
		return nil
	}
	seq := binary.BigEndian.Uint32(p[4:])
	total := int(binary.BigEndian.Uint32(p[8:]))
	i := int(binary.BigEndian.Uint16(p[12:]))
	count := int(binary.BigEndian.Uint16(p[14:]))
	if total < 8 || total > MaxFrame || count != (total+ChunkSize-1)/ChunkSize || i >= count {
		return nil
	}
	want := ChunkSize
	if i == count-1 {
		want = total - i*ChunkSize
	}
	if len(p)-16 != want {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started && seq < a.seq {
		return nil
	}
	if !a.started || seq > a.seq {
		a.started = true
		a.seq = seq
		a.done = false
		a.total = total
		a.count = count
		a.parts = map[int][]byte{}
	}
	if a.done || a.total != total || a.count != count {
		return nil
	}
	a.parts[i] = append([]byte(nil), p[16:]...)
	if len(a.parts) != count {
		return nil
	}
	var b bytes.Buffer
	for j := 0; j < count; j++ {
		io.Copy(&b, bytes.NewReader(a.parts[j]))
	}
	a.parts = nil
	a.done = true
	return b.Bytes()
}
