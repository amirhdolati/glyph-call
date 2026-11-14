package connection

import (
	"context"
	"testing"
	"time"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
)

func caps() session.Caps {
	return session.Caps{Graphic: true, Unicode: true, Color: true, Columns: 140, Rows: 45, Rates: []int{8000, 16000, 24000, 48000}, Effects: []string{"real", "robot", "8bit", "radio"}, Audio: true, Camera: true}
}
func connectPeers(t *testing.T, r session.Request, hostCaps, guestCaps session.Caps) (*Peer, *Peer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	host, err := New("host", hostCaps, r, "")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := New("guest", guestCaps, r, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close(); guest.Close() })
	if err = host.CreateChannels(); err != nil {
		t.Fatal(err)
	}
	offer, err := host.LocalDescription(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = guest.SetRemote(offer); err != nil {
		t.Fatal(err)
	}
	answer, err := guest.LocalDescription(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = host.SetRemote(answer); err != nil {
		t.Fatal(err)
	}
	if err = host.Wait(ctx, host.Ready); err != nil {
		t.Fatal(err)
	}
	if err = guest.Wait(ctx, guest.Ready); err != nil {
		t.Fatal(err)
	}
	return host, guest
}
func waitFor(t *testing.T, test func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !test() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestRealDTLSPeersAgreeHostPolicyAndSendDifferentBestVideoModes(t *testing.T) {
	r := session.DefaultRequest()
	r.Mode = "graphic"
	r.Voice = "robot"
	limited := caps()
	limited.Graphic = false
	limited.Unicode = false
	limited.Rates = []int{16000}
	host, guest := connectPeers(t, r, caps(), limited)
	if host.Decision() != guest.Decision() {
		t.Fatal("settings differ")
	}
	d := host.Decision()
	if d.ForGuest.Mode != "fun" || d.ForGuest.Style != "ascii" || d.ForHost.Mode != "graphic" || d.Audio.Rate != 16000 || d.Audio.Effect != "robot" {
		t.Fatal("host policy was not applied")
	}
	a, _ := host.VerificationCode()
	b, _ := guest.VerificationCode()
	if a != b {
		t.Fatal("certificate codes differ")
	}
	frame, _ := media.MakeFrame(media.Demo(0), d.ForGuest)
	if host.SendVideo(frame) {
		t.Fatal("video sent before approval")
	}
	if err := host.Approve(); err != nil {
		t.Fatal(err)
	}
	if err := guest.Approve(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		select {
		case <-host.Approved:
			return true
		default:
			return false
		}
	})
	waitFor(t, func() bool {
		select {
		case <-guest.Approved:
			return true
		default:
			return false
		}
	})
	if !host.SendVideo(frame) {
		t.Fatal("fun frame not queued")
	}
	back, _ := media.MakeFrame(media.Demo(1), d.ForHost)
	if !guest.SendVideo(back) {
		t.Fatal("graphic frame not queued")
	}
	waitFor(t, func() bool { return guest.RemoteFrame() != nil && host.RemoteFrame() != nil })
	if guest.RemoteFrame().Mode != "fun" || host.RemoteFrame().Mode != "graphic" {
		t.Fatal("per-receiver adaptation failed")
	}
	for i := uint32(0); i < 3; i++ {
		if !host.SendAudio(media.AudioPacket(i, make([]byte, 640))) {
			t.Fatal("audio send failed")
		}
	}
	waitFor(t, func() bool { host.mu.Lock(); defer host.mu.Unlock(); return host.localApproved && host.remoteApproved })
	if len(guest.AudioRead()) != media.DeviceSamples {
		t.Fatal("audio playout format invalid")
	}
}
func TestHiddenWireRejectsClearImageEvenAfterApproval(t *testing.T) {
	r := session.DefaultRequest()
	r.Mode = "hidden"
	host, guest := connectPeers(t, r, caps(), caps())
	host.Approve()
	guest.Approve()
	waitFor(t, func() bool {
		select {
		case <-host.Approved:
			return true
		default:
			return false
		}
	})
	d := host.Decision()
	clear, _ := media.MakeFrame(media.Demo(0), session.Video{Mode: "graphic", Style: "image", Width: 640, Height: 360})
	if host.SendVideo(clear) {
		t.Fatal("host sent clear image against hidden policy")
	}
	hidden, _ := media.MakeFrame(media.Demo(0), d.ForGuest)
	if !host.SendVideo(hidden) {
		t.Fatal("hidden frame blocked")
	}
	waitFor(t, func() bool { return guest.RemoteFrame() != nil })
	if guest.RemoteFrame().Mode != "hidden" {
		t.Fatal("hidden mode upgraded")
	}
}
func TestGuestCannotOverrideSettings(t *testing.T) {
	p, err := New("host", caps(), session.DefaultRequest(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err = p.handleControl([]byte(`{"protocol":"ascii-call/go/1","kind":"settings","decision":{}}`)); err == nil {
		t.Fatal("guest settings accepted")
	}
}
