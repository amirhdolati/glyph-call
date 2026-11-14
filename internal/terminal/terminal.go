package terminal

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
	"golang.org/x/term"
)

const esc = "\x1b"
const kittyQuery = "\x1b_Gi=74291,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"

var terminalQuery = query

func Size() (int, int) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 100, 30
	}
	return max(16, min(1000, w)), max(8, min(500, h))
}
func ProbeGraphics(preference string) (string, string) {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	candidate := preference
	if candidate == "auto" {
		switch {
		case program == "ghostty" || os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("TERM") == "xterm-kitty":
			candidate = "kitty"
		case program == "iterm.app" || program == "wezterm":
			candidate = "iterm2"
		default:
			return "", "terminal has no recognized image protocol"
		}
	}
	if os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		return "", "native graphics disabled inside tmux/screen"
	}
	request := kittyQuery
	if candidate == "iterm2" {
		if program != "iterm.app" && program != "wezterm" {
			return "", "iTerm2 protocol not confirmed by terminal identity"
		}
		request = "\x1b]1337;ReportCellSize\x07"
	}
	response := terminalQuery(request)
	pattern := `\x1b_Gi=74291(?:,[^;\x1b]*)?;OK\x1b\\`
	if candidate == "iterm2" {
		pattern = `\x1b\]1337;ReportCellSize=[0-9.]+;[0-9.]+(?:;[0-9.]+)?(?:\x07|\x1b\\)`
	}
	if !regexp.MustCompile(pattern).Match(response) {
		return "", "terminal did not confirm native image support; using character rendering"
	}
	return candidate, ""
}
func Capabilities(graphics string, noAudio, noCamera bool) (session.Caps, string, string) {
	protocol, reason := ProbeGraphics(graphics)
	w, h := Size()
	locale := strings.ToUpper(os.Getenv("LC_ALL") + os.Getenv("LC_CTYPE") + os.Getenv("LANG"))
	unicode := strings.Contains(locale, "UTF") || os.Getenv("TERM_PROGRAM") != ""
	color := os.Getenv("TERM") != "dumb"
	return session.Caps{Graphic: protocol != "", Unicode: unicode, Color: color, Columns: w, Rows: h, Rates: []int{8000, 16000, 24000, 48000}, Effects: []string{"real", "robot", "8bit", "radio", "off"}, Audio: !noAudio, Camera: !noCamera}, protocol, reason
}

func Kitty(png []byte, id, w, h int) []byte {
	encoded := base64.StdEncoding.EncodeToString(png)
	var out strings.Builder
	for offset := 0; offset < len(encoded); offset += 4096 {
		end := min(len(encoded), offset+4096)
		more := 1
		if end == len(encoded) {
			more = 0
		}
		meta := ""
		if offset == 0 {
			meta = fmt.Sprintf("a=T,f=100,t=d,i=%d,p=1,c=%d,r=%d,C=1,q=2,", id, w, h)
		}
		fmt.Fprintf(&out, "%s_G%sm=%d;%s%s\\", esc, meta, more, encoded[offset:end], esc)
	}
	return []byte(out.String())
}
func ITerm(png []byte, w, h int) []byte {
	return []byte(fmt.Sprintf("%s]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=1:%s\x07", esc, len(png), w, h, base64.StdEncoding.EncodeToString(png)))
}

type Display struct {
	mu             sync.Mutex
	Out            io.Writer
	Protocol       string
	previous       [2]*media.Frame
	width, height  int
	started        bool
	Size           func() (int, int)
	restoreInput   func()
	graphicsLayout bool
}

func (d *Display) Start() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return
	}
	if d.Out == nil {
		d.Out = os.Stdout
	}
	d.restoreInput = suspendInputEcho()
	fmt.Fprint(d.Out, esc+"[?1049h"+esc+"[?6l"+esc+"[r"+esc+"[?7l"+esc+"[?25l"+esc+"[0m"+esc+"[2J"+esc+"[H")
	d.started = true
}
func (d *Display) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.started {
		return
	}
	if d.restoreInput != nil {
		defer d.restoreInput()
		d.restoreInput = nil
	}
	if d.Protocol == "kitty" {
		for _, id := range []int{73101, 73102} {
			fmt.Fprintf(d.Out, "%s_Ga=d,d=I,i=%d,q=2;%s\\", esc, id, esc)
		}
	}
	fmt.Fprint(d.Out, esc+"[?2026l"+esc+"[?7h"+esc+"[0m"+esc+"[?25h"+esc+"[?1049l")
	d.started = false
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return -1
		}
		return r
	}, s)
}

func colorSequence(rgb uint32) string {
	program := strings.ToLower(os.Getenv("TERM_PROGRAM"))
	if strings.Contains(strings.ToLower(os.Getenv("COLORTERM")), "truecolor") || program == "ghostty" || program == "iterm.app" || program == "wezterm" || os.Getenv("KITTY_WINDOW_ID") != "" {
		return fmt.Sprintf("%s[38;2;%d;%d;%dm", esc, rgb>>16, (rgb>>8)&255, rgb&255)
	}
	if strings.Contains(os.Getenv("TERM"), "256") {
		r, g, b := int(rgb>>16), int((rgb>>8)&255), int(rgb&255)
		return fmt.Sprintf("%s[38;5;%dm", esc, 16+(r*5/255)*36+(g*5/255)*6+b*5/255)
	}
	index := 0
	if rgb>>16 > 127 {
		index |= 1
	}
	if (rgb>>8)&255 > 127 {
		index |= 2
	}
	if rgb&255 > 127 {
		index |= 4
	}
	return fmt.Sprintf("%s[%dm", esc, 30+index)
}
func (d *Display) Draw(local, remote *media.Frame, status string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	getSize := d.Size
	if getSize == nil {
		getSize = Size
	}
	w, h := getSize()
	if w < 30 || h < 10 {
		return fmt.Errorf("enlarge terminal to at least 30 columns and 10 rows")
	}
	var out bytes.Buffer
	out.WriteString(esc + "[?2026h")
	graphics := local != nil && local.Mode == "graphic" || remote != nil && remote.Mode == "graphic"
	if d.width != w || d.height != h || d.graphicsLayout != graphics {
		out.WriteString(esc + "[2J")
		d.previous = [2]*media.Frame{}
		d.width = w
		d.height = h
		d.graphicsLayout = graphics
	}
	paneW := (w - 6) / 2
	paneH := h - 5
	fmt.Fprintf(&out, "%s[1;1H%s[2K", esc, esc)
	for index, title := range []string{"YOU", "PEER"} {
		fmt.Fprintf(&out, "%s[1;%dH%s", esc, 2+index*(paneW+3), title)
	}
	if !graphics {
		fmt.Fprintf(&out, "%s[2;1H%s[2K %s + %s", esc, esc, strings.Repeat("-", paneW), strings.Repeat("-", paneW))
		// Compose both panes into each row. No independent cursor streams and no
		// line reaches the terminal's last column, even with automatic wrapping.
		for y := 0; y < paneH; y++ {
			fmt.Fprintf(&out, "%s[%d;1H%s[2K ", esc, y+3, esc)
			writePaneRow(&out, local, paneW, paneH, y, "Camera off / unavailable")
			out.WriteString(" | ")
			writePaneRow(&out, remote, paneW, paneH, y, "Waiting for peer...")
		}
	} else {
		for index, frame := range []*media.Frame{local, remote} {
			column := 2 + index*(paneW+3)
			title := []string{"YOU", "PEER"}[index]
			fmt.Fprintf(&out, "%s[1;%dH%s", esc, column, title)
			if d.previous[index] == frame && frame != nil {
				continue
			}
			if frame == nil {
				message := "Waiting for peer..."
				if index == 0 {
					message = "Camera off / unavailable"
				}
				fmt.Fprintf(&out, "%s[3;%dH%-*s", esc, column, paneW, message[:min(len(message), paneW)])
				continue
			}
			if frame.Mode == "graphic" {
				if d.Protocol == "" {
					return fmt.Errorf("graphic frame reached an unverified terminal")
				}
				png, err := frame.PNG()
				if err != nil {
					continue
				}
				fmt.Fprintf(&out, "%s[3;%dH", esc, column)
				if d.Protocol == "kitty" {
					out.Write(Kitty(png, 73101+index, paneW, paneH))
				} else {
					out.Write(ITerm(png, paneW, paneH))
				}
			} else {
				for y := 0; y < paneH; y++ {
					fmt.Fprintf(&out, "%s[%d;%dH", esc, y+3, column)
					writePaneRow(&out, frame, paneW, paneH, y, "")
				}
			}
			d.previous[index] = frame
		}
	}
	status = clean(status)
	if len(status) > w-1 {
		status = status[:w-1]
	}
	fmt.Fprintf(&out, "%s[%d;1H%s[2K%s", esc, h, esc, status)
	out.WriteString(esc + "[?2026l")
	_, err := d.Out.Write(out.Bytes())
	return err
}

func writePaneRow(out *bytes.Buffer, frame *media.Frame, width, height, row int, placeholder string) {
	out.WriteString(esc + "[0m")
	if frame == nil || frame.Width < 1 || frame.Height < 1 || len(frame.Cells) != frame.Width*frame.Height {
		text := ""
		if row == height/2 {
			text = placeholder
		}
		text = text[:min(len(text), width)]
		out.WriteString(text)
		out.WriteString(strings.Repeat(" ", width-len(text)))
		return
	}
	scale := min(1.0, min(float64(width)/float64(frame.Width), float64(height)/float64(frame.Height)))
	w, h := max(1, int(math.Round(float64(frame.Width)*scale))), max(1, int(math.Round(float64(frame.Height)*scale)))
	w = min(width, w)
	h = min(height, h)
	left, top := (width-w)/2, (height-h)/2
	var previous uint32
	colored := false
	for x := 0; x < width; x++ {
		if row < top || row >= top+h || x < left || x >= left+w {
			out.WriteByte(' ')
			continue
		}
		sy := (row - top) * (frame.Height - 1) / max(1, h-1)
		sx := (x - left) * (frame.Width - 1) / max(1, w-1)
		cell := frame.Cells[sy*frame.Width+sx]
		if frame.Mode == "fun" && (!colored || cell.RGB != previous) {
			out.WriteString(colorSequence(cell.RGB))
			previous = cell.RGB
			colored = true
		}
		out.WriteRune(cell.Rune)
	}
	out.WriteString(esc + "[0m")
}
