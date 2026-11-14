// Command makepreviews renders dual-pane call demos through Glyph Call character modes.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"strings"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type modeSpec struct {
	name  string
	label string
	video session.Video
	scale int
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
		{name: "hidden", label: "Hidden", video: session.Video{Mode: "hidden", Style: "ascii", Width: 32, Height: 16, FPS: 8}, scale: 3},
		{name: "fun-ascii", label: "Fun ASCII", video: session.Video{Mode: "fun", Style: "ascii", Color: true, Width: 56, Height: 24, FPS: 12}, scale: 1},
		{name: "fun-unicode", label: "Fun Unicode", video: session.Video{Mode: "fun", Style: "unicode", Color: true, Width: 56, Height: 24, FPS: 12}, scale: 1},
	}

	people, err := loadGIF(srcPeople)
	if err != nil {
		return err
	}
	anim, err := loadGIF(srcAnim)
	if err != nil {
		return err
	}

	for _, mode := range modes {
		path := filepath.Join(outDir, "mode-"+mode.name+".gif")
		if err := writeCallGIF(people, mode, path, 28); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	// Second source through Fun Unicode so both test clips appear as call demos.
	animPath := filepath.Join(outDir, "mode-fun-unicode-animated.gif")
	if err := writeCallGIF(anim, modes[2], animPath, 28); err != nil {
		return err
	}
	fmt.Println("wrote", animPath)

	// Drop superseded single-pane assets if present.
	for _, name := range []string{"call-demo.gif", "animated-demo.gif", "video-modes.png"} {
		_ = os.Remove(filepath.Join(outDir, name))
	}
	return nil
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

func writeCallGIF(frames []*image.Paletted, mode modeSpec, path string, maxFrames int) error {
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
	var bounds image.Rectangle
	for i, src := range frames {
		left, right := splitPanes(src)
		local, err := media.MakeFrame(left, mode.video)
		if err != nil {
			return err
		}
		remote, err := media.MakeFrame(right, mode.video)
		if err != nil {
			return err
		}
		status := fmt.Sprintf("%s  |  DTLS  |  preview demo", mode.label)
		rgba := renderCall(local, remote, status, mode.scale)
		if i == 0 {
			bounds = rgba.Bounds()
		} else if rgba.Bounds() != bounds {
			rgba = fitExact(rgba, bounds.Dx(), bounds.Dy())
		}
		paletted := image.NewPaletted(bounds, palette)
		draw.FloydSteinberg.Draw(paletted, bounds, rgba, image.Point{})
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

func splitPanes(src image.Image) (image.Image, image.Image) {
	b := src.Bounds()
	mid := b.Min.X + b.Dx()/2
	left := image.NewRGBA(image.Rect(0, 0, mid-b.Min.X, b.Dy()))
	right := image.NewRGBA(image.Rect(0, 0, b.Max.X-mid, b.Dy()))
	draw.Draw(left, left.Bounds(), src, b.Min, draw.Src)
	draw.Draw(right, right.Bounds(), src, image.Pt(mid, b.Min.Y), draw.Src)
	return left, right
}

func renderCall(local, remote media.Frame, status string, scale int) *image.RGBA {
	if scale < 1 {
		scale = 1
	}
	face := basicfont.Face7x13
	cellW, cellH := face.Advance*scale, face.Height*scale
	gap := 3 * scale
	pad := 8 * scale
	titleH := face.Height*scale + 6*scale
	ruleH := face.Height * scale
	statusH := face.Height*scale + 8*scale

	paneW := max(local.Width, remote.Width) * cellW
	paneH := max(local.Height, remote.Height) * cellH
	width := pad*2 + paneW*2 + gap*3
	height := pad + titleH + ruleH + paneH + statusH + pad
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{10, 12, 16, 255}
	chrome := color.RGBA{160, 170, 180, 255}
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	drawText(dst, face, pad, pad+face.Ascent*scale, "YOU", chrome, scale)
	drawText(dst, face, pad+paneW+gap*2, pad+face.Ascent*scale, "PEER", chrome, scale)

	ruleY := pad + titleH
	rule := strings.Repeat("-", max(8, paneW/(face.Advance*scale)))
	drawText(dst, face, pad, ruleY+face.Ascent*scale, rule+" + "+rule, chrome, scale)

	originY := ruleY + ruleH
	blitFrame(dst, local, pad, originY, cellW, cellH, scale, face)
	blitFrame(dst, remote, pad+paneW+gap*2, originY, cellW, cellH, scale, face)

	drawText(dst, face, pad, originY+paneH+6*scale+face.Ascent*scale, status, color.RGBA{120, 200, 140, 255}, scale)
	return dst
}

func blitFrame(dst *image.RGBA, frame media.Frame, originX, originY, cellW, cellH, scale int, face *basicfont.Face) {
	for y := 0; y < frame.Height; y++ {
		for x := 0; x < frame.Width; x++ {
			c := frame.Cells[y*frame.Width+x]
			rgb := c.RGB
			if rgb == 0 {
				rgb = 0xffffff
			}
			fg := color.RGBA{uint8(rgb >> 16), uint8(rgb >> 8), uint8(rgb), 255}
			drawGlyph(dst, face, originX+x*cellW, originY+y*cellH, string(c.Rune), fg, scale)
		}
	}
}

func drawText(dst *image.RGBA, face *basicfont.Face, x, baseline int, text string, fg color.RGBA, scale int) {
	if scale == 1 {
		d := &font.Drawer{Dst: dst, Src: image.NewUniform(fg), Face: face, Dot: fixed.P(x, baseline)}
		d.DrawString(text)
		return
	}
	// Draw into a 1x buffer then nearest-scale each glyph row for Hidden demos.
	tmp := image.NewRGBA(image.Rect(0, 0, face.Advance*len([]rune(text))+2, face.Height+2))
	draw.Draw(tmp, tmp.Bounds(), &image.Uniform{C: color.RGBA{10, 12, 16, 255}}, image.Point{}, draw.Src)
	d := &font.Drawer{Dst: tmp, Src: image.NewUniform(fg), Face: face, Dot: fixed.P(0, face.Ascent)}
	d.DrawString(text)
	for py := 0; py < face.Height; py++ {
		for px := 0; px < tmp.Bounds().Dx(); px++ {
			_, _, _, a := tmp.At(px, py).RGBA()
			if a == 0 {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					dst.Set(x+px*scale+dx, baseline-face.Ascent*scale+py*scale+dy, fg)
				}
			}
		}
	}
}

func drawGlyph(dst *image.RGBA, face *basicfont.Face, x, y int, text string, fg color.RGBA, scale int) {
	if scale == 1 {
		d := &font.Drawer{Dst: dst, Src: image.NewUniform(fg), Face: face, Dot: fixed.P(x, y+face.Ascent)}
		d.DrawString(text)
		return
	}
	tmp := image.NewRGBA(image.Rect(0, 0, face.Advance+2, face.Height+2))
	draw.Draw(tmp, tmp.Bounds(), &image.Uniform{C: color.RGBA{10, 12, 16, 255}}, image.Point{}, draw.Src)
	d := &font.Drawer{Dst: tmp, Src: image.NewUniform(fg), Face: face, Dot: fixed.P(0, face.Ascent)}
	d.DrawString(text)
	for py := 0; py < face.Height; py++ {
		for px := 0; px < face.Advance; px++ {
			_, _, _, a := tmp.At(px, py).RGBA()
			if a == 0 {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					dst.Set(x+px*scale+dx, y+py*scale+dy, fg)
				}
			}
		}
	}
}

func fitExact(src *image.RGBA, width, height int) *image.RGBA {
	b := src.Bounds()
	if b.Dx() == width && b.Dy() == height {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.RGBA{10, 12, 16, 255}}, image.Point{}, draw.Src)
	draw.Draw(dst, image.Rect(0, 0, min(width, b.Dx()), min(height, b.Dy())), src, b.Min, draw.Src)
	return dst
}

func compactPalette() color.Palette {
	palette := color.Palette{color.RGBA{10, 12, 16, 255}, color.White}
	for r := 0; r < 256; r += 51 {
		for g := 0; g < 256; g += 51 {
			for b := 0; b < 256; b += 51 {
				palette = append(palette, color.RGBA{uint8(r), uint8(g), uint8(b), 255})
			}
		}
	}
	return palette
}
