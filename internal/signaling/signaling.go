package signaling

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/amirhdolati/glyph-call/internal/connection"
	"github.com/amirhdolati/glyph-call/internal/session"
	"github.com/pion/webrtc/v4"
	"github.com/psanford/wormhole-william/wormhole"
)

const Limit = 65536
const RoomApp = "ascii-call/go/invite/1"

func validCode(code string) bool {
	return len(code) <= 256 && regexp.MustCompile(`^[0-9]+-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*$`).MatchString(code)
}

type Envelope struct {
	Protocol  string `json:"protocol"`
	Type      string `json:"type"`
	SDP       string `json:"sdp"`
	ReplyApp  string `json:"reply_app,omitempty"`
	ReplyCode string `json:"reply_code,omitempty"`
}

func encode(e Envelope) ([]byte, error) {
	e.Protocol = session.Protocol
	data, err := json.Marshal(e)
	if len(data) > Limit {
		return nil, errors.New("signaling message too large")
	}
	return data, err
}
func decode(data []byte, kind string) (Envelope, error) {
	var e Envelope
	if len(data) > Limit {
		return e, errors.New("signaling message too large")
	}
	if err := json.Unmarshal(data, &e); err != nil {
		return e, err
	}
	if e.Protocol != session.Protocol || e.Type != kind || e.SDP == "" {
		return e, errors.New("wrong signaling type/version; both users must run the Go version")
	}
	return e, nil
}
func description(e Envelope) webrtc.SessionDescription {
	kind := webrtc.SDPTypeOffer
	if e.Type == "answer" {
		kind = webrtc.SDPTypeAnswer
	}
	return webrtc.SessionDescription{Type: kind, SDP: e.SDP}
}
func WriteFile(path string, e Envelope) error {
	data, err := encode(e)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}
func ReadFile(path, kind string) (Envelope, error) {
	f, err := os.Open(path)
	if err != nil {
		return Envelope{}, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, Limit+1))
	if err != nil {
		return Envelope{}, err
	}
	return decode(data, kind)
}
func FileHost(ctx context.Context, p *connection.Peer, out, answer string) error {
	if _, err := os.Stat(answer); err == nil {
		return errors.New("answer file already exists: choose fresh paths")
	}
	if err := p.CreateChannels(); err != nil {
		return err
	}
	desc, err := p.LocalDescription(ctx, true)
	if err != nil {
		return err
	}
	if err = WriteFile(out, Envelope{Type: "offer", SDP: desc.SDP}); err != nil {
		return err
	}
	fmt.Printf("Send %s privately; save the answer at %s. Keep this terminal open.\n", out, answer)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(answer); err == nil {
			e, err := ReadFile(answer, "answer")
			if err != nil {
				return err
			}
			return p.SetRemote(description(e))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func FileGuest(ctx context.Context, p *connection.Peer, offer, out string) error {
	e, err := ReadFile(offer, "offer")
	if err != nil {
		return err
	}
	if err = p.SetRemote(description(e)); err != nil {
		return err
	}
	desc, err := p.LocalDescription(ctx, false)
	if err != nil {
		return err
	}
	err = WriteFile(out, Envelope{Type: "answer", SDP: desc.SDP})
	if err == nil {
		fmt.Printf("Send %s privately to the host. Keep this terminal open.\n", out)
	}
	return err
}

func client(relay, app string) *wormhole.Client {
	return &wormhole.Client{AppID: app, RendezvousURL: relay, PassPhraseComponentLength: 4}
}
func receive(ctx context.Context, c *wormhole.Client, code, kind string) (Envelope, error) {
	incoming, err := c.Receive(ctx, strings.TrimSpace(code))
	if err != nil {
		return Envelope{}, err
	}
	if incoming.Type != wormhole.TransferText {
		return Envelope{}, errors.New("only signaling text is accepted")
	}
	data, err := io.ReadAll(io.LimitReader(incoming, Limit+1))
	if err != nil {
		return Envelope{}, err
	}
	return decode(data, kind)
}
func sent(ctx context.Context, status chan wormhole.SendResult) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-status:
		if result.Error != nil {
			return result.Error
		}
		if !result.OK {
			return errors.New("encrypted setup message not acknowledged")
		}
		return nil
	}
}
func ValidateRelay(relay string) error {
	if relay == "" {
		return nil
	}
	u, err := url.Parse(relay)
	if err != nil || u.Host == "" || (u.Scheme != "ws" && u.Scheme != "wss") {
		return errors.New("mailbox URL must be ws:// or wss://")
	}
	return nil
}

// Two PAKE-protected text exchanges; a per-call reply namespace prevents
// collisions. Users share only the first code; no file/transit relay is used.
func RoomHost(ctx context.Context, p *connection.Peer, relay string, onCode func(string)) error {
	if err := ValidateRelay(relay); err != nil {
		return err
	}
	if err := p.CreateChannels(); err != nil {
		return err
	}
	desc, err := p.LocalDescription(ctx, true)
	if err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return err
	}
	replyApp := "ascii-call/go/reply/" + hex.EncodeToString(secret[:16])
	replyCode := "1-" + hex.EncodeToString(secret[16:])
	data, err := encode(Envelope{Type: "offer", SDP: desc.SDP, ReplyApp: replyApp, ReplyCode: replyCode})
	if err != nil {
		return err
	}
	c := client(relay, RoomApp)
	code, status, err := c.SendText(ctx, string(data))
	if err != nil {
		return err
	}
	if !validCode(code) {
		return errors.New("mailbox returned an invalid room code")
	}
	onCode(code)
	answer, err := receive(ctx, client(relay, replyApp), replyCode, "answer")
	if err != nil {
		return err
	}
	if err = sent(ctx, status); err != nil {
		return err
	}
	return p.SetRemote(description(answer))
}
func RoomGuest(ctx context.Context, p *connection.Peer, relay, code string) error {
	code = strings.TrimSpace(code)
	if !validCode(code) {
		return errors.New("invalid room code")
	}
	if err := ValidateRelay(relay); err != nil {
		return err
	}
	offer, err := receive(ctx, client(relay, RoomApp), code, "offer")
	if err != nil {
		return err
	}
	if !regexp.MustCompile(`^ascii-call/go/reply/[0-9a-f]{32}$`).MatchString(offer.ReplyApp) || !regexp.MustCompile(`^1-[0-9a-f]{32}$`).MatchString(offer.ReplyCode) {
		return errors.New("invalid encrypted reply route")
	}
	if err = p.SetRemote(description(offer)); err != nil {
		return err
	}
	desc, err := p.LocalDescription(ctx, false)
	if err != nil {
		return err
	}
	data, err := encode(Envelope{Type: "answer", SDP: desc.SDP})
	if err != nil {
		return err
	}
	_, status, err := client(relay, offer.ReplyApp).SendText(ctx, string(data), wormhole.WithCode(offer.ReplyCode))
	if err != nil {
		return err
	}
	return sent(ctx, status)
}
