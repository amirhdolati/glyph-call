package terminal

import (
	"bytes"
	"strings"
	"testing"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/hinshun/vt10x"
)

func TestFunPanesStaySeparateAcrossUpdatesAndResize(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	var output bytes.Buffer
	w, h := 120, 36
	d := Display{Out: &output, Size: func() (int, int) { return w, h }}
	vt := vt10x.New(vt10x.WithSize(w, h))
	d.Start()
	defer d.Close()
	for _, size := range [][2]int{{120, 36}, {48, 16}, {31, 10}, {160, 44}} {
		w, h = size[0], size[1]
		vt.Resize(w, h)
		for update := 0; update < 3; update++ {
			leftRune, rightRune := rune(0x2801+update), rune(0x28f0+update)
			frame := func(r rune) *media.Frame {
				f := &media.Frame{Mode: "fun", Style: "unicode", Width: 54, Height: 27}
				for i := 0; i < f.Width*f.Height; i++ {
					f.Cells = append(f.Cells, media.Cell{Rune: r, RGB: uint32(i * 123)})
				}
				return f
			}
			left, right := frame(leftRune), frame(rightRune)
			// The outer edge must survive downscaling, rather than being clipped.
			left.Cells[len(left.Cells)-1].Rune = 0x2820
			right.Cells[len(right.Cells)-1].Rune = 0x28e0
			if err := d.Draw(left, right, "fun/unicode | DTLS encrypted"); err != nil {
				t.Fatal(err)
			}
			if _, err := vt.Write(output.Bytes()); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			paneW, paneH := (w-6)/2, h-5
			counts := map[rune]int{}
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					r := vt.Cell(x, y).Char
					if r >= 0x2800 && r <= 0x28ff || r == 0x2820 || r == 0x28e0 {
						counts[r]++
						wantLeft := r == leftRune || r == 0x2820
						if y < 2 || y >= 2+paneH || wantLeft && (x < 1 || x >= 1+paneW) || !wantLeft && (x < paneW+4 || x >= 2*paneW+4) {
							t.Fatalf("glyph %q outside its pane at %d,%d (%dx%d)\n%s", r, x, y, w, h, vt.String())
						}
					}
				}
			}
			if counts[leftRune] == 0 || counts[rightRune] == 0 || counts[0x2820] != 1 || counts[0x28e0] != 1 {
				t.Fatalf("panes missing, stale or clipped: %v\n%s", counts, vt.String())
			}
			if vt.Cell(1, 0).Char != 'Y' || vt.Cell(paneW+4, 0).Char != 'P' || !strings.Contains(vt.String(), "DTLS") {
				t.Fatalf("header/status scrolled or overwritten\n%s", vt.String())
			}
		}
	}
}
