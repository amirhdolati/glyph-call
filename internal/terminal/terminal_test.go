package terminal

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
)

func TestUnsupportedGraphicsAlwaysFallsBackInsteadOfPrintingImage(t *testing.T) {
	old := terminalQuery
	defer func() { terminalQuery = old }()
	terminalQuery = func(request string) []byte { return []byte("\x1b[?64;1;2c") }
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	protocol, reason := ProbeGraphics("kitty")
	if protocol != "" || reason == "" {
		t.Fatal("manual graphics bypassed verification")
	}
	terminalQuery = func(request string) []byte { return []byte("\x1b_Gi=74291;OK\x1b\\\x1b[?62c") }
	protocol, _ = ProbeGraphics("kitty")
	if protocol != "kitty" {
		t.Fatal("valid graphics response rejected")
	}
}
func TestKittyPacketsContainOnlyBoundedBase64ImageChunks(t *testing.T) {
	png := append([]byte("\x89PNG"), bytes.Repeat([]byte{0, 1, 255, 27}, 5000)...)
	command := Kitty(png, 42, 40, 20)
	parts := regexp.MustCompile(`\x1b_G([^;]*);([^\x1b]*)\x1b\\`).FindAllSubmatch(command, -1)
	var payload strings.Builder
	for _, p := range parts {
		if len(p[2]) > 4096 {
			t.Fatal("oversized terminal chunk")
		}
		payload.Write(p[2])
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil || !bytes.Equal(decoded, png) {
		t.Fatal("native image payload corrupted")
	}
	if !bytes.Contains(parts[0][1], []byte("q=2")) {
		t.Fatal("unsolicited terminal replies not suppressed")
	}
}
func TestHiddenDisplayHasNoGraphicUploadOrUnicodeFace(t *testing.T) {
	frame, _ := media.MakeFrame(media.Demo(0), session.Video{Mode: "hidden", Style: "ascii", Width: 32, Height: 16, FPS: 8})
	var output bytes.Buffer
	d := Display{Out: &output}
	d.Start()
	if err := d.Draw(&frame, &frame, "hidden"); err != nil {
		t.Fatal(err)
	}
	d.Close()
	raw := output.Bytes()
	if bytes.Contains(raw, []byte("_G")) || bytes.Contains(raw, []byte("]1337;File")) {
		t.Fatal("hidden display uploaded an image")
	}
	if !bytes.HasSuffix(raw, []byte("\x1b[0m\x1b[?25h\x1b[?1049l")) {
		t.Fatal("terminal state not restored")
	}
}
