package session

import (
	"errors"
	"fmt"
	"slices"
)

const Protocol = "ascii-call/go/1"

type Request struct {
	Mode     string `json:"mode"`
	FunStyle string `json:"fun_style"`
	Voice    string `json:"voice"`
	Rate     int    `json:"rate"`
	FPS      int    `json:"fps"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func DefaultRequest() Request { return Request{"fun", "unicode", "real", 24000, 12, 1280, 720} }
func (r Request) Validate() error {
	if !slices.Contains([]string{"hidden", "fun", "graphic"}, r.Mode) || !slices.Contains([]string{"ascii", "unicode"}, r.FunStyle) || !slices.Contains([]string{"real", "robot", "8bit", "radio", "off"}, r.Voice) || !slices.Contains([]int{8000, 16000, 24000, 48000}, r.Rate) || r.FPS < 1 || r.FPS > 20 || r.Width < 16 || r.Width > 1280 || r.Height < 8 || r.Height > 720 {
		return errors.New("invalid host settings")
	}
	return nil
}

type Caps struct {
	Graphic bool     `json:"graphic"`
	Unicode bool     `json:"unicode"`
	Color   bool     `json:"color"`
	Columns int      `json:"columns"`
	Rows    int      `json:"rows"`
	Rates   []int    `json:"rates"`
	Effects []string `json:"effects"`
	Audio   bool     `json:"audio"`
	Camera  bool     `json:"camera"`
}

func (c Caps) Validate() error {
	if c.Columns < 16 || c.Columns > 1000 || c.Rows < 8 || c.Rows > 500 || len(c.Rates) > 4 || len(c.Effects) > 5 {
		return errors.New("invalid peer capabilities")
	}
	for _, r := range c.Rates {
		if !slices.Contains([]int{8000, 16000, 24000, 48000}, r) {
			return errors.New("invalid audio rate")
		}
	}
	for _, e := range c.Effects {
		if !slices.Contains([]string{"real", "robot", "8bit", "radio", "off"}, e) {
			return errors.New("invalid audio effect")
		}
	}
	return nil
}

type Video struct {
	Mode   string `json:"mode"`
	Style  string `json:"style"`
	Color  bool   `json:"color"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    int    `json:"fps"`
}
type Audio struct {
	Effect string `json:"effect"`
	Rate   int    `json:"rate"`
}
type Decision struct {
	Requested Request `json:"requested"`
	ForHost   Video   `json:"for_host"`
	ForGuest  Video   `json:"for_guest"`
	Audio     Audio   `json:"audio"`
}

func videoFor(r Request, c Caps) Video {
	v := Video{Mode: r.Mode, Style: r.FunStyle, Color: c.Color, Width: r.Width, Height: r.Height, FPS: r.FPS}
	if v.Mode == "graphic" && !c.Graphic {
		v.Mode = "fun"
	}
	switch v.Mode {
	case "hidden":
		v.Style = "ascii"
		v.Color = false
		v.Width = min(32, min(r.Width, max(8, (c.Columns-8)/2)))
		v.Height = min(16, min(r.Height, max(4, c.Rows-5)))
		v.FPS = min(v.FPS, 8)
	case "fun":
		if !c.Unicode {
			v.Style = "ascii"
		}
		v.Width = min(120, min(max(8, (c.Columns-8)/2), r.Width))
		v.Height = min(48, min(max(4, c.Rows-5), r.Height))
	case "graphic":
		v.Style = "image"
		v.Color = true
	}
	return v
}

// The host's mode is a ceiling: hidden can never silently become fun/graphic.
// Each receiver gets its own best view; a limited guest need not downgrade the host.
func Negotiate(r Request, host, guest Caps) (Decision, error) {
	if err := r.Validate(); err != nil {
		return Decision{}, err
	}
	if err := host.Validate(); err != nil {
		return Decision{}, err
	}
	if err := guest.Validate(); err != nil {
		return Decision{}, err
	}
	d := Decision{Requested: r, ForHost: videoFor(r, host), ForGuest: videoFor(r, guest), Audio: Audio{"off", 8000}}
	if !host.Audio || !guest.Audio || r.Voice == "off" {
		return d, nil
	}
	for _, rate := range []int{48000, 24000, 16000, 8000} {
		if rate <= r.Rate && slices.Contains(host.Rates, rate) && slices.Contains(guest.Rates, rate) {
			d.Audio.Rate = rate
			break
		}
	}
	if !slices.Contains(host.Rates, d.Audio.Rate) || !slices.Contains(guest.Rates, d.Audio.Rate) {
		return d, nil
	}
	order := map[string][]string{"real": {"real"}, "robot": {"robot", "8bit", "radio"}, "8bit": {"8bit", "robot", "radio"}, "radio": {"radio", "8bit"}}
	for _, effect := range order[r.Voice] {
		if slices.Contains(host.Effects, effect) && slices.Contains(guest.Effects, effect) {
			d.Audio.Effect = effect
			break
		}
	}
	return d, nil
}

func (d Decision) Summary() string {
	return fmt.Sprintf("host: %s/%s %dx%d | guest: %s/%s %dx%d | voice: %s %d Hz", d.ForHost.Mode, d.ForHost.Style, d.ForHost.Width, d.ForHost.Height, d.ForGuest.Mode, d.ForGuest.Style, d.ForGuest.Width, d.ForGuest.Height, d.Audio.Effect, d.Audio.Rate)
}
