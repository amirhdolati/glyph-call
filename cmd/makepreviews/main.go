// Command makepreviews renders demo GIFs through Glyph Call character modes.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type modeSpec struct {
	label string
	video session.Video
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	srcPeople := filepath.Join(root, "docs/media/call-demo.gif")
	srcAnim := filepath.Join(root, "docs/media/animated-demo.gif")
	if env := os.Getenv("GLYPH_CALL_PREVIEW_SRC"); env != "" {
		srcPeople = filepath.Join(env, "call-demo.gif")
		srcAnim = filepath.Join(env, "animated-demo.gif")
	}
	outDir := filepath.Join(root, "docs/media")

	modes := []modeSpec{
		{label: "Hidden", video: session.Video{Mode: "hidden", Style: "ascii", Width: 32, Height: 16, FPS: 8}},
		{label: "Fun ASCII", video: session.Video{Mode: "fun", Style: "ascii", Color: true, Width: 72, Height: 28, FPS: 12}},
		{label: "Fun Unicode", video: session.Video{Mode: "fun", Style: "unicode", Color: true, Width: 72, Height: 28, FPS: 12}},
	}

	people, err := loadGIF(srcPeople)
	if err != nil {
		return err
	}
	anim, err := loadGIF(srcAnim)
	if err != nil {
		return err
	}

	ascii := modes[1].video
	if err := writeModeGIF(people, ascii, filepath.Join(outDir, "call-demo.gif"), 32); err != nil {
		return err
	}
	if err := writeModeGIF(anim, ascii, filepath.Join(outDir, "animated-demo.gif"), 32); err != nil {
		return err
	}
	frame := people[min(12, len(people)-1)]
	return writeModesPNG(frame, modes, filepath.Join(outDir, "video-modes.png"))
}

func loadGIF(path string) ([]*image.Paletted, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, err
	}
	return g.Image, nil
}

func writeModeGIF(frames []*image.Paletted, profile session.Video, path string, maxFrames int) error {
	if maxFrames > 0 && len(frames) > maxFrames {
		step := max(1, len(frames)/maxFrames)
		trimmed := make([]*image.Paletted, 0, maxFrames)
		for i := 0; i < len(frames) && len(trimmed) < maxFrames; i += step {
			trimmed = append(trimmed, frames[i])
		}
		frames = trimmed
	}
	out := &gif.GIF{LoopCount: 0}
	palette := compactPalette()
	for _, src := range frames {
		cellFrame, err := media.MakeFrame(src, profile)
		if err != nil {
			return err
		}
		rgba := renderFrame(cellFrame, "")
		paletted := image.NewPaletted(rgba.Bounds(), palette)
		draw.FloydSteinberg.Draw(paletted, rgba.Bounds(), rgba, image.Point{})
		out.Image = append(out.Image, paletted)
		out.Delay = append(out.Delay, 12)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.EncodeAll(f, out)
}

func writeModesPNG(src image.Image, modes []modeSpec, path string) error {
	const gap = 24
	const targetH = 360
	panels := make([]*image.RGBA, len(modes))
	totalW := gap
	for i, mode := range modes {
		frame, err := media.MakeFrame(src, mode.video)
		if err != nil {
			return err
		}
		panel := renderFrame(frame, mode.label)
		if mode.video.Mode == "hidden" {
			panel = nearestScale(panel, 3)
		}
		panels[i] = fitHeight(panel, targetH)
		totalW += panels[i].Bounds().Dx() + gap
	}
	canvas := image.NewRGBA(image.Rect(0, 0, totalW, targetH+gap*2))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{12, 14, 18, 255}}, image.Point{}, draw.Src)
	x := gap
	for _, panel := range panels {
		y := gap + (targetH-panel.Bounds().Dy())/2
		r := image.Rect(x, y, x+panel.Bounds().Dx(), y+panel.Bounds().Dy())
		draw.Draw(canvas, r, panel, image.Point{}, draw.Src)
		x += panel.Bounds().Dx() + gap
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	return enc.Encode(f, canvas)
}

func fitHeight(src *image.RGBA, height int) *image.RGBA {
	b := src.Bounds()
	if b.Dy() == height {
		return src
	}
	width := max(1, b.Dx()*height/b.Dy())
	return nearestSize(src, width, height)
}

func nearestScale(src *image.RGBA, factor int) *image.RGBA {
	b := src.Bounds()
	return nearestSize(src, b.Dx()*factor, b.Dy()*factor)
}

func nearestSize(src *image.RGBA, width, height int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		sy := b.Min.Y + y*b.Dy()/height
		for x := 0; x < width; x++ {
			sx := b.Min.X + x*b.Dx()/width
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}

func renderFrame(frame media.Frame, label string) *image.RGBA {
	face := basicfont.Face7x13
	cellW, cellH := face.Advance, face.Height
	padX, padY := 8, 8
	labelH := 0
	if label != "" {
		labelH = cellH + 10
	}
	w := frame.Width*cellW + padX*2
	h := frame.Height*cellH + padY*2 + labelH
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	bg := color.RGBA{10, 12, 16, 255}
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	d := &font.Drawer{Dst: dst, Face: face}
	if label != "" {
		d.Src = image.NewUniform(color.RGBA{200, 210, 220, 255})
		d.Dot = fixed.P(padX, padY+face.Ascent)
		d.DrawString(label)
	}
	originY := padY + labelH
	for y := 0; y < frame.Height; y++ {
		for x := 0; x < frame.Width; x++ {
			c := frame.Cells[y*frame.Width+x]
			rgb := c.RGB
			if rgb == 0 {
				rgb = 0xffffff
			}
			d.Src = image.NewUniform(color.RGBA{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb), 255})
			d.Dot = fixed.P(padX+x*cellW, originY+y*cellH+face.Ascent)
			d.DrawString(string(c.Rune))
		}
	}
	return dst
}

func compactPalette() color.Palette {
	palette := color.Palette{color.RGBA{10, 12, 16, 255}, color.White}
	for _, step := range []int{0, 64, 128, 192, 255} {
		for r := 0; r < 256; r += 64 {
			for g := 0; g < 256; g += 64 {
				for b := 0; b < 256; b += 64 {
					palette = append(palette, color.RGBA{uint8(r), uint8(g), uint8(b), 255})
				}
			}
		}
		_ = step
		break
	}
	return palette
}
