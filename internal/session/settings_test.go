package session

import "testing"

func capabilities() Caps {
	return Caps{Graphic: true, Unicode: true, Color: true, Columns: 160, Rows: 50, Rates: []int{8000, 16000, 24000, 48000}, Effects: []string{"real", "robot", "8bit", "radio"}, Audio: true, Camera: true}
}
func TestHostPolicyAndPerReceiverFallback(t *testing.T) {
	host, guest := capabilities(), capabilities()
	guest.Graphic = false
	guest.Unicode = false
	guest.Rates = []int{8000, 16000}
	r := DefaultRequest()
	r.Mode = "graphic"
	r.Voice = "robot"
	d, err := Negotiate(r, host, guest)
	if err != nil {
		t.Fatal(err)
	}
	if d.ForHost.Mode != "graphic" || d.ForGuest.Mode != "fun" || d.ForGuest.Style != "ascii" || d.Audio.Effect != "robot" || d.Audio.Rate != 16000 {
		t.Fatalf("unexpected decision: %+v", d)
	}
}
func TestHiddenNeverUpgradesOrAddsColor(t *testing.T) {
	r := DefaultRequest()
	r.Mode = "hidden"
	r.Width = 16
	r.Height = 8
	d, err := Negotiate(r, capabilities(), capabilities())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []Video{d.ForHost, d.ForGuest} {
		if v.Mode != "hidden" || v.Style != "ascii" || v.Color || v.Width > 16 || v.Height > 8 || v.FPS > 8 {
			t.Fatalf("hidden policy leaked detail: %+v", v)
		}
	}
}
func TestUnsupportedMaskingEffectNeverBecomesRealVoice(t *testing.T) {
	r := DefaultRequest()
	r.Voice = "robot"
	guest := capabilities()
	guest.Effects = []string{"real"}
	d, err := Negotiate(r, capabilities(), guest)
	if err != nil {
		t.Fatal(err)
	}
	if d.Audio.Effect != "off" {
		t.Fatal("masking effect silently changed to real voice")
	}
	guest.Effects = []string{"8bit", "real"}
	d, _ = Negotiate(r, capabilities(), guest)
	if d.Audio.Effect != "8bit" {
		t.Fatal("closest supported effect was not chosen")
	}
}
func TestInvalidHostAndPeerCapabilitiesRejected(t *testing.T) {
	r := DefaultRequest()
	r.Mode = "invalid"
	if _, err := Negotiate(r, capabilities(), capabilities()); err == nil {
		t.Fatal("invalid mode accepted")
	}
	r = DefaultRequest()
	guest := capabilities()
	guest.Rates = []int{12345}
	if _, err := Negotiate(r, capabilities(), guest); err == nil {
		t.Fatal("invalid rates accepted")
	}
}
