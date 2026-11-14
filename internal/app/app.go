package app

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amirhdolati/glyph-call/internal/connection"
	"github.com/amirhdolati/glyph-call/internal/devices"
	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
	"github.com/amirhdolati/glyph-call/internal/signaling"
	"github.com/amirhdolati/glyph-call/internal/terminal"
)

const Version = "1.0.1"

var stdin = bufio.NewReader(os.Stdin)

type options struct {
	command, code, graphics, relay, stun, out, offer, answer, quality string
	camera, seconds, timeout                                          int
	noAudio, noCamera, noStun, synthetic                              bool
	request                                                           session.Request
}

func prompt(question string) (string, error) {
	fmt.Print(question)
	line, err := stdin.ReadString('\n')
	return strings.TrimSpace(line), err
}
func menu() ([]string, error) {
	fmt.Println("\nGlyph Call\n1. Host a call\n2. Join a call\n3. Camera preview\n0. Exit")
	choice, err := prompt("Choose: ")
	if err != nil {
		return nil, err
	}
	switch choice {
	case "0":
		return nil, nil
	case "2":
		return []string{"connect"}, nil
	case "3":
		return []string{"preview"}, nil
	case "1":
		fmt.Println("Video: 1 hidden (coarse ASCII), 2 fun (ASCII/Unicode art), 3 graphic (real camera)")
		mode, err := prompt("Video [2]: ")
		if err != nil {
			return nil, err
		}
		modes := map[string]string{"": "fun", "1": "hidden", "2": "fun", "3": "graphic"}
		selected, ok := modes[mode]
		if !ok {
			return nil, errors.New("invalid video choice")
		}
		fmt.Println("Voice: 1 real, 2 robot, 3 8bit")
		voice, err := prompt("Voice [1]: ")
		if err != nil {
			return nil, err
		}
		voices := map[string]string{"": "real", "1": "real", "2": "robot", "3": "8bit"}
		effect, ok := voices[voice]
		if !ok {
			return nil, errors.New("invalid voice choice")
		}
		return []string{"host", "--mode", selected, "--voice", effect}, nil
	default:
		return nil, errors.New("choose 0, 1, 2 or 3")
	}
}

func parse(args []string) (options, error) {
	o := options{request: session.DefaultRequest()}
	if len(args) == 0 {
		return o, errors.New("missing command")
	}
	o.command = args[0]
	if !strings.Contains("|host|connect|preview|cameras|offer|join|", "|"+o.command+"|") {
		return o, errors.New("commands: host, connect, preview, cameras, offer, join")
	}
	f := flag.NewFlagSet(o.command, flag.ContinueOnError)
	f.StringVar(&o.request.Mode, "mode", o.request.Mode, "host video mode: hidden, fun, graphic")
	f.StringVar(&o.request.FunStyle, "fun-style", "unicode", "fun style: unicode or ascii")
	f.StringVar(&o.request.Voice, "voice", "real", "host voice effect: real, robot, 8bit, radio, off")
	f.StringVar(&o.quality, "audio-quality", "24k", "host audio quality: 8k, 16k, 24k, 48k")
	f.IntVar(&o.request.FPS, "fps", 12, "host maximum video frames per second (1..20)")
	f.IntVar(&o.request.Width, "width", 1280, "host maximum image width (character modes have safe lower limits)")
	f.IntVar(&o.request.Height, "height", 720, "host maximum image height")
	f.StringVar(&o.graphics, "graphics", "auto", "terminal image protocol: auto, kitty, iterm2; always verified")
	f.StringVar(&o.relay, "relay-url", "", "custom Magic Wormhole mailbox ws:// or wss://")
	f.StringVar(&o.stun, "stun", "", "custom STUN URL; code-based calls default to Google STUN")
	f.BoolVar(&o.noStun, "no-stun", false, "disable public STUN")
	f.BoolVar(&o.noAudio, "no-audio", false, "disable local microphone and speaker")
	f.BoolVar(&o.noCamera, "no-camera", false, "disable local camera")
	f.BoolVar(&o.synthetic, "synthetic", false, "use generated video without camera or microphone")
	f.IntVar(&o.camera, "camera", 0, "camera index; list with cameras")
	f.IntVar(&o.seconds, "seconds", 30, "preview duration in seconds")
	f.IntVar(&o.timeout, "timeout", 300, "connection/setup timeout in seconds")
	f.StringVar(&o.out, "out", "", "manual signaling output path")
	f.StringVar(&o.offer, "offer", "go-offer.json", "manual offer file")
	f.StringVar(&o.answer, "answer", "go-answer.json", "manual answer file")
	if err := f.Parse(args[1:]); err != nil {
		return o, err
	}
	if f.NArg() > 1 || f.NArg() > 0 && o.command != "connect" {
		return o, errors.New("unexpected argument; put flags before the optional room code")
	}
	if f.NArg() == 1 {
		o.code = f.Arg(0)
	}
	switch o.quality {
	case "low", "8k":
		o.request.Rate = 8000
	case "16k":
		o.request.Rate = 16000
	case "balanced", "24k":
		o.request.Rate = 24000
	case "high", "48k":
		o.request.Rate = 48000
	default:
		return o, errors.New("audio quality must be 8k, 16k, 24k or 48k")
	}
	if err := o.request.Validate(); err != nil {
		return o, err
	}
	if o.camera < 0 || o.seconds < 1 || o.seconds > 3600 || o.timeout < 10 || o.timeout > 3600 {
		return o, errors.New("invalid camera, duration or timeout")
	}
	if o.graphics != "auto" && o.graphics != "kitty" && o.graphics != "iterm2" {
		return o, errors.New("graphics must be auto, kitty or iterm2")
	}
	if o.noStun && o.stun != "" {
		return o, errors.New("choose --stun or --no-stun")
	}
	if o.stun != "" && !strings.HasPrefix(o.stun, "stun:") {
		return o, errors.New("STUN URL must start with stun:")
	}
	if err := signaling.ValidateRelay(o.relay); err != nil {
		return o, err
	}
	if o.command == "connect" || o.command == "join" {
		var forbidden string
		f.Visit(func(item *flag.Flag) {
			switch item.Name {
			case "mode", "fun-style", "voice", "audio-quality", "fps", "width", "height":
				forbidden = item.Name
			}
		})
		if forbidden != "" {
			return o, fmt.Errorf("the host controls --%s; the client adapts automatically", forbidden)
		}
	}
	if o.out == "" {
		o.out = "go-offer.json"
		if o.command == "join" {
			o.out = "go-answer.json"
		}
	}
	if o.synthetic {
		o.noAudio, o.noCamera = true, true
	}
	return o, nil
}

func Run(args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Glyph Call\nCommands: host, connect, preview, cameras, offer, join\nRun without arguments for the interactive menu.\nExample: glyph-call host --mode fun --voice 8bit\nUse: glyph-call host --help")
		return nil
	}
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Println("Glyph Call", Version)
		return nil
	}
	if len(args) == 0 || len(args) == 1 && args[0] == "menu" {
		var err error
		args, err = menu()
		if err != nil || args == nil {
			return err
		}
	}
	o, err := parse(args)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.command == "cameras" {
		names := devices.Cameras()
		if len(names) == 0 {
			return errors.New("no native cameras found; native capture currently targets macOS 14+")
		}
		for i, name := range names {
			fmt.Printf("[%d] %s\n", i, name)
		}
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	caps, protocol, reason := terminal.Capabilities(o.graphics, o.noAudio || !devices.Supported(), o.noCamera || !devices.Supported())
	if o.synthetic {
		caps.Camera = true // Generated frames are available without a capture device.
	}
	if reason != "" && o.request.Mode == "graphic" {
		fmt.Println("Display fallback:", reason)
	}
	if o.command == "preview" {
		decision, err := session.Negotiate(o.request, caps, caps)
		if err != nil {
			return err
		}
		fmt.Println("Preview:", decision.Summary())
		return preview(ctx, o, decision.ForHost, protocol)
	}
	if o.command == "connect" && o.code == "" {
		o.code, err = prompt("One-time room code: ")
		if err != nil {
			return err
		}
		if o.code == "" {
			return errors.New("room code is required")
		}
	}
	role := "host"
	if o.command == "connect" || o.command == "join" {
		role = "guest"
	}
	stun := o.stun
	if (o.command == "host" || o.command == "connect") && !o.noStun && stun == "" {
		stun = "stun:stun.l.google.com:19302"
	}
	if o.command == "host" || o.command == "connect" {
		fmt.Printf("Encrypted setup: Magic Wormhole | STUN: %s | media: direct WebRTC\n", valueOr(stun, "off"))
	}
	p, err := connection.New(role, caps, o.request, stun)
	if err != nil {
		return err
	}
	defer p.Close()
	setup, stop := context.WithTimeout(ctx, time.Duration(o.timeout)*time.Second)
	defer stop()
	switch o.command {
	case "host":
		err = signaling.RoomHost(setup, p, o.relay, func(code string) { fmt.Printf("\nShare privately: %s\nPeer runs: glyph-call connect\n\n", code) })
	case "connect":
		err = signaling.RoomGuest(setup, p, o.relay, o.code)
	case "offer":
		err = signaling.FileHost(setup, p, o.out, o.answer)
	case "join":
		err = signaling.FileGuest(setup, p, o.offer, o.out)
	}
	if err != nil {
		return err
	}
	if err = p.Wait(setup, p.Ready); err != nil {
		return err
	}
	decision := p.Decision()
	fmt.Println("Agreed settings:", decision.Summary())
	code, err := p.VerificationCode()
	if err != nil {
		return err
	}
	fmt.Printf("\nCompare every group through an already trusted channel:\n%s\n", code)
	reply := make(chan string, 1)
	go func() { line, _ := prompt("Codes match? Type yes to enable your devices: "); reply <- line }()
	select {
	case <-setup.Done():
		return setup.Err()
	case <-p.Stopped:
		return p.Error()
	case line := <-reply:
		if strings.ToLower(line) != "yes" {
			fmt.Println("Cancelled; devices were not opened.")
			return nil
		}
	}
	if err = p.Approve(); err != nil {
		return err
	}
	fmt.Println("Waiting for the other participant to approve...")
	if err = p.Wait(setup, p.Approved); err != nil {
		return err
	}
	stop()
	return callMedia(ctx, o, p, protocol)
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
func mirror(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	if rgba, ok := src.(*image.RGBA); ok {
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				source := rgba.PixOffset(b.Min.X+b.Dx()-1-x, b.Min.Y+y)
				target := y*dst.Stride + x*4
				copy(dst.Pix[target:target+4], rgba.Pix[source:source+4])
			}
		}
		return dst
	}
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dst.Set(x, y, src.At(b.Min.X+b.Dx()-1-x, b.Min.Y+y))
		}
	}
	return dst
}

func preview(ctx context.Context, o options, profile session.Video, protocol string) error {
	var camera *devices.Camera
	var err error
	if !o.synthetic {
		camera, err = devices.OpenCamera(o.camera)
		if err != nil {
			return err
		}
		defer camera.Close()
	}
	display := terminal.Display{Protocol: protocol}
	display.Start()
	defer display.Close()
	ticker := time.NewTicker(time.Second / time.Duration(profile.FPS))
	defer ticker.Stop()
	deadline := time.NewTimer(time.Duration(o.seconds) * time.Second)
	defer deadline.Stop()
	start := time.Now()
	counter := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-ticker.C:
			var img image.Image
			if o.synthetic {
				img = media.Demo(counter)
				counter += 5
			} else {
				img, err = camera.Read()
				if err != nil {
					return err
				}
				if img == nil {
					if time.Since(start) > 8*time.Second {
						return errors.New("camera produced no frames")
					}
					continue
				}
				img = mirror(img)
			}
			frame, err := media.MakeFrame(img, profile)
			if err != nil {
				return err
			}
			if err = display.Draw(&frame, &frame, fmt.Sprintf("LOCAL %s/%s | no network or microphone | Ctrl+C: exit", profile.Mode, profile.Style)); err != nil {
				return err
			}
		}
	}
}

func callMedia(ctx context.Context, o options, p *connection.Peer, protocol string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	receive, send := p.Profiles()
	decision := p.Decision()
	notice := ""
	var camera *devices.Camera
	if !o.noCamera && devices.Supported() {
		var err error
		camera, err = devices.OpenCamera(o.camera)
		if err != nil {
			notice = "camera unavailable"
			fmt.Fprintln(os.Stderr, "Camera:", err)
		} else {
			defer camera.Close()
		}
	}
	audioDone := make(chan struct{})
	if decision.Audio.Effect != "off" && !o.noAudio {
		audio, err := devices.OpenAudio()
		if err != nil {
			notice = "audio unavailable"
			fmt.Fprintln(os.Stderr, "Audio:", err)
			close(audioDone)
		} else {
			go func() {
				defer close(audioDone)
				defer audio.Close()
				voice := media.Voice{Mode: decision.Audio.Effect}
				var sequence uint32
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-p.Stopped:
						return
					case <-ticker.C:
						if samples := audio.Read(); samples != nil {
							packet := media.AudioPacket(sequence, voice.Encode(samples, decision.Audio.Rate))
							p.SendAudio(packet)
							sequence++
						}
						audio.Write(p.AudioRead())
					}
				}
			}()
		}
	} else {
		close(audioDone)
	}
	defer func() { cancel(); <-audioDone }()
	display := terminal.Display{Protocol: protocol}
	display.Start()
	defer display.Close()
	ticker := time.NewTicker(time.Second / time.Duration(max(receive.FPS, send.FPS)))
	defer ticker.Stop()
	var local *media.Frame
	counter := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.Stopped:
			return p.Failure()
		case <-ticker.C:
			var img image.Image
			if o.synthetic {
				img = media.Demo(counter)
				counter += 5
			} else if camera != nil {
				var err error
				img, err = camera.Read()
				if err != nil {
					notice = "camera read failed"
				}
			}
			if img != nil {
				out, err := media.MakeFrame(img, send)
				if err == nil {
					p.SendVideo(out)
				}
				own, err := media.MakeFrame(mirror(img), receive)
				if err == nil {
					local = &own
				}
			}
			status := fmt.Sprintf("%s/%s | voice %s %s kHz | DTLS encrypted | %s | Ctrl+C: end", receive.Mode, receive.Style, decision.Audio.Effect, strconv.Itoa(decision.Audio.Rate/1000), notice)
			if err := display.Draw(local, p.RemoteFrame(), status); err != nil {
				return err
			}
		}
	}
}
