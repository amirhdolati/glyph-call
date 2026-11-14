package app

import (
	"bufio"
	"strings"
	"testing"
)

func TestHostModesEffectsAndQualities(t *testing.T) {
	for _, mode := range []string{"hidden", "fun", "graphic"} {
		for _, voice := range []string{"real", "robot", "8bit", "radio", "off"} {
			o, err := parse([]string{"host", "--mode", mode, "--voice", voice, "--audio-quality", "16k"})
			if err != nil || o.request.Mode != mode || o.request.Voice != voice || o.request.Rate != 16000 {
				t.Fatal("host flags not applied", err)
			}
		}
	}
}
func TestClientCannotChooseHostSettings(t *testing.T) {
	for _, arg := range []string{"--mode", "--voice", "--audio-quality", "--fps"} {
		value := map[string]string{"--mode": "fun", "--voice": "robot", "--audio-quality": "16k", "--fps": "8"}[arg]
		if _, err := parse([]string{"connect", arg, value}); err == nil {
			t.Fatalf("client changed %s", arg)
		}
	}
}
func TestClientDeviceOptOutRemainsLocal(t *testing.T) {
	o, err := parse([]string{"connect", "--no-audio", "--no-camera", "1-code-words"})
	if err != nil || !o.noAudio || !o.noCamera || o.code != "1-code-words" {
		t.Fatal("client opt-out ignored", err)
	}
}

func TestMenuHostOnlyAsksVideoAndVoice(t *testing.T) {
	previous := stdin
	defer func() { stdin = previous }()
	// Exactly three answers: host, fun, robot. No style or quality question.
	stdin = bufio.NewReader(strings.NewReader("1\n2\n2\n"))
	args, err := menu()
	if err != nil {
		t.Fatal(err)
	}
	o, err := parse(args)
	if err != nil || o.request.Mode != "fun" || o.request.Voice != "robot" || o.request.Rate != 24000 {
		t.Fatalf("unexpected simple menu: %+v %v", o, err)
	}
}
func TestSyntheticCallsNeverOpenPhysicalDevices(t *testing.T) {
	for _, args := range [][]string{{"host", "--synthetic"}, {"connect", "--synthetic", "1-test-room"}} {
		o, err := parse(args)
		if err != nil || !o.noCamera || !o.noAudio {
			t.Fatal("synthetic call can open devices", err)
		}
	}
}
