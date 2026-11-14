package signaling

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amirhdolati/glyph-call/internal/connection"
	"github.com/amirhdolati/glyph-call/internal/session"
)

func TestManualSignalingRejectsOverwriteAndWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invite.json")
	e := Envelope{Type: "offer", SDP: "example SDP"}
	if err := WriteFile(path, e); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, e); err == nil {
		t.Fatal("overwrote invitation")
	}
	if _, err := ReadFile(path, "answer"); err == nil {
		t.Fatal("wrong role accepted")
	}
	if _, err := decode([]byte(`{"version":1,"type":"offer","sdp":"legacy"}`), "offer"); err == nil {
		t.Fatal("legacy signaling accepted")
	}
}
func TestNativeGoWormholeRoundtrip(t *testing.T) {
	relay := os.Getenv("ASCII_CALL_TEST_RELAY")
	if relay == "" {
		t.Skip("set ASCII_CALL_TEST_RELAY to a local standard mailbox to run PAKE integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	caps := session.Caps{Columns: 120, Rows: 40, Unicode: true, Color: true, Rates: []int{8000, 16000, 24000, 48000}, Effects: []string{"real", "robot", "8bit", "radio"}}
	request := session.DefaultRequest()
	request.Mode = "hidden"
	host, err := connection.New("host", caps, request, "")
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	guest, err := connection.New("guest", caps, request, "")
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	code := make(chan string, 1)
	result := make(chan error, 1)
	go func() { result <- RoomHost(ctx, host, relay, func(value string) { code <- value }) }()
	var invitation string
	select {
	case invitation = <-code:
	case err := <-result:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := RoomGuest(ctx, guest, relay, invitation); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := host.Wait(ctx, host.Ready); err != nil {
		t.Fatal(err)
	}
	if err := guest.Wait(ctx, guest.Ready); err != nil {
		t.Fatal(err)
	}
	a, _ := host.VerificationCode()
	b, _ := guest.VerificationCode()
	if a != b {
		t.Fatal("PAKE-delivered DTLS fingerprints differ")
	}
	if host.Decision() != guest.Decision() || host.Decision().ForGuest.Mode != "hidden" {
		t.Fatal("host settings not negotiated over room connection")
	}
}
