package connection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/amirhdolati/glyph-call/internal/media"
	"github.com/amirhdolati/glyph-call/internal/session"
	"github.com/pion/sctp"
	"github.com/pion/webrtc/v4"
)

type control struct {
	Protocol string            `json:"protocol"`
	Kind     string            `json:"kind"`
	Role     string            `json:"role,omitempty"`
	Caps     *session.Caps     `json:"caps,omitempty"`
	Request  *session.Request  `json:"request,omitempty"`
	Decision *session.Decision `json:"decision,omitempty"`
	Hash     string            `json:"hash,omitempty"`
}

type Peer struct {
	PC                                              *webrtc.PeerConnection
	Role                                            string
	caps                                            session.Caps
	request                                         session.Request
	mu                                              sync.Mutex
	channels                                        map[string]*webrtc.DataChannel
	remoteCaps                                      session.Caps
	remoteHello, helloSent, settingsAck, configured bool
	decision                                        session.Decision
	localApproved, remoteApproved                   bool
	remoteFrame                                     *media.Frame
	audio                                           *media.AudioBuffer
	err                                             error
	Ready, Approved, Stopped                        chan struct{}
	readyOnce, approvedOnce, stopOnce               sync.Once
	ctx                                             context.Context
	cancel                                          context.CancelFunc
	videoBusy                                       atomic.Bool
	sequence                                        atomic.Uint32
	chunks                                          media.Assembler
	wg                                              sync.WaitGroup
}

func New(role string, caps session.Caps, request session.Request, stun string) (*Peer, error) {
	if role != "host" && role != "guest" {
		return nil, errors.New("invalid role")
	}
	if err := caps.Validate(); err != nil {
		return nil, err
	}
	if role == "host" {
		if err := request.Validate(); err != nil {
			return nil, err
		}
	}
	config := webrtc.Configuration{}
	if stun != "" {
		config.ICEServers = []webrtc.ICEServer{{URLs: []string{stun}}}
	}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Peer{PC: pc, Role: role, caps: caps, request: request, channels: map[string]*webrtc.DataChannel{}, Ready: make(chan struct{}), Approved: make(chan struct{}), Stopped: make(chan struct{}), ctx: ctx, cancel: cancel}
	pc.OnDataChannel(p.attach)
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed {
			p.stop(errors.New("direct WebRTC connection failed"))
		} else if state == webrtc.PeerConnectionStateClosed {
			p.stop(nil)
		}
	})
	return p, nil
}
func (p *Peer) stop(err error) {
	p.stopOnce.Do(func() {
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.Stopped)
		p.cancel()
	})
}
func (p *Peer) Error() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	return errors.New("peer disconnected")
}
func (p *Peer) Failure() error { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Peer) CreateChannels() error {
	ordered := false
	zero := uint16(0)
	lifetime := uint16(3000)
	for _, item := range []struct {
		name    string
		options *webrtc.DataChannelInit
	}{{"control", nil}, {"audio", &webrtc.DataChannelInit{Ordered: &ordered, MaxRetransmits: &zero}}, {"video", &webrtc.DataChannelInit{Ordered: &ordered, MaxPacketLifeTime: &lifetime}}} {
		ch, err := p.PC.CreateDataChannel(item.name, item.options)
		if err != nil {
			return err
		}
		p.attach(ch)
	}
	return nil
}
func (p *Peer) attach(ch *webrtc.DataChannel) {
	if ch.Label() != "control" && ch.Label() != "audio" && ch.Label() != "video" {
		_ = ch.Close()
		return
	}
	p.mu.Lock()
	if _, exists := p.channels[ch.Label()]; exists {
		p.mu.Unlock()
		_ = ch.Close()
		return
	}
	p.channels[ch.Label()] = ch
	p.mu.Unlock()
	ch.OnOpen(p.onOpen)
	ch.OnClose(func() { p.stop(nil) })
	ch.OnError(func(err error) {
		// Pion closes an active SCTP association with a user-initiated ABORT.
		// The remote participant ending a call is a normal hangup.
		if errors.Is(err, sctp.ErrUserInitiatedAbort) {
			err = nil
		}
		p.stop(err)
	})
	ch.OnMessage(func(message webrtc.DataChannelMessage) {
		if ch.Label() == "control" {
			if err := p.handleControl(message.Data); err != nil {
				p.stop(err)
			}
			return
		}
		p.receive(ch.Label(), message.Data)
	})
	if ch.ReadyState() == webrtc.DataChannelStateOpen {
		p.onOpen()
	}
}
func (p *Peer) sendControlLocked(message control) error {
	message.Protocol = session.Protocol
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	channel := p.channels["control"]
	if channel == nil || channel.ReadyState() != webrtc.DataChannelStateOpen {
		return errors.New("control channel not open")
	}
	return channel.SendText(string(data))
}
func (p *Peer) markReadyLocked() {
	if !p.configured || !p.settingsAck || len(p.channels) != 3 {
		return
	}
	for _, ch := range p.channels {
		if ch.ReadyState() != webrtc.DataChannelStateOpen {
			return
		}
	}
	p.readyOnce.Do(func() { close(p.Ready) })
}
func (p *Peer) onOpen() {
	p.mu.Lock()
	err := p.helloLocked()
	p.markReadyLocked()
	p.mu.Unlock()
	if err != nil {
		p.stop(err)
	}
}

func (p *Peer) helloLocked() error {
	ch := p.channels["control"]
	if ch != nil && ch.ReadyState() == webrtc.DataChannelStateOpen && !p.helloSent {
		p.helloSent = true
		msg := control{Kind: "hello", Role: p.Role, Caps: &p.caps}
		if p.Role == "host" {
			msg.Request = &p.request
		}
		return p.sendControlLocked(msg)
	}
	return nil
}
func decisionHash(d session.Decision) string {
	raw, _ := json.Marshal(d)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func (p *Peer) handleControl(raw []byte) error {
	if len(raw) > 16384 {
		return errors.New("control message too large")
	}
	var msg control
	if err := json.Unmarshal(raw, &msg); err != nil {
		return errors.New("incompatible client: both participants must run the Go version")
	}
	if msg.Protocol != session.Protocol {
		return errors.New("incompatible protocol: update both participants")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch msg.Kind {
	case "hello":
		if p.remoteHello || msg.Role == p.Role || (msg.Role != "host" && msg.Role != "guest") || msg.Caps == nil {
			return errors.New("invalid peer role or repeated capabilities")
		}
		if err := msg.Caps.Validate(); err != nil {
			return err
		}
		if err := p.helloLocked(); err != nil {
			return err
		}
		p.remoteCaps = *msg.Caps
		p.remoteHello = true
		if p.Role == "host" {
			decision, err := session.Negotiate(p.request, p.caps, p.remoteCaps)
			if err != nil {
				return err
			}
			p.decision = decision
			p.configured = true
			p.audio = media.NewAudioBuffer(decision.Audio.Rate)
			return p.sendControlLocked(control{Kind: "settings", Decision: &decision})
		}
		if msg.Request == nil {
			return errors.New("host settings missing")
		}
		if err := msg.Request.Validate(); err != nil {
			return err
		}
		p.request = *msg.Request
	case "settings":
		if p.Role != "guest" || !p.remoteHello || p.configured || msg.Decision == nil {
			return errors.New("unauthorized settings change")
		}
		expected, err := session.Negotiate(p.request, p.remoteCaps, p.caps)
		if err != nil {
			return err
		}
		if decisionHash(expected) != decisionHash(*msg.Decision) {
			return errors.New("host decision exceeds negotiated capabilities")
		}
		p.decision = expected
		p.configured = true
		p.settingsAck = true
		p.audio = media.NewAudioBuffer(expected.Audio.Rate)
		if err := p.sendControlLocked(control{Kind: "settings-ack", Hash: decisionHash(expected)}); err != nil {
			return err
		}
		p.markReadyLocked()
	case "settings-ack":
		if p.Role != "host" || !p.configured || msg.Hash != decisionHash(p.decision) {
			return errors.New("settings acknowledgement mismatch")
		}
		p.settingsAck = true
		p.markReadyLocked()
	case "approved":
		if !p.configured || !p.settingsAck {
			return errors.New("approval before settings")
		}
		p.remoteApproved = true
		p.approvedOnce.Do(func() { close(p.Approved) })
	default:
		return errors.New("unknown control message")
	}
	return nil
}
func (p *Peer) Approve() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.configured || !p.settingsAck {
		return errors.New("settings not agreed")
	}
	p.localApproved = true
	return p.sendControlLocked(control{Kind: "approved"})
}
func (p *Peer) Decision() session.Decision { p.mu.Lock(); defer p.mu.Unlock(); return p.decision }
func (p *Peer) Profiles() (receive, send session.Video) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Role == "host" {
		return p.decision.ForHost, p.decision.ForGuest
	}
	return p.decision.ForGuest, p.decision.ForHost
}
func (p *Peer) receive(label string, data []byte) {
	p.mu.Lock()
	allowed := p.localApproved && p.remoteApproved && p.configured
	profile := p.decision.ForHost
	if p.Role == "guest" {
		profile = p.decision.ForGuest
	}
	audio := p.audio
	effect := p.decision.Audio.Effect
	p.mu.Unlock()
	if !allowed {
		return
	}
	if label == "audio" {
		if effect != "off" && audio != nil {
			audio.Push(data)
		}
		return
	}
	if payload := p.chunks.Push(data); payload != nil {
		frame, err := media.ParseFrame(payload, profile)
		if err == nil {
			p.mu.Lock()
			p.remoteFrame = &frame
			p.mu.Unlock()
		}
	}
}
func (p *Peer) RemoteFrame() *media.Frame { p.mu.Lock(); defer p.mu.Unlock(); return p.remoteFrame }
func (p *Peer) AudioRead() []int16 {
	p.mu.Lock()
	buffer := p.audio
	p.mu.Unlock()
	if buffer == nil {
		return make([]int16, media.DeviceSamples)
	}
	return buffer.Read()
}
func (p *Peer) SendAudio(packet []byte) bool {
	p.mu.Lock()
	allowed := p.localApproved && p.remoteApproved && p.decision.Audio.Effect != "off"
	ch := p.channels["audio"]
	p.mu.Unlock()
	if !allowed || ch == nil || ch.ReadyState() != webrtc.DataChannelStateOpen || ch.BufferedAmount() > 8192 {
		return false
	}
	return ch.Send(packet) == nil
}
func (p *Peer) SendVideo(frame media.Frame) bool {
	p.mu.Lock()
	allowed := p.localApproved && p.remoteApproved
	ch := p.channels["video"]
	profile := p.decision.ForGuest
	if p.Role == "guest" {
		profile = p.decision.ForHost
	}
	p.mu.Unlock()
	if !allowed || ch == nil || ch.ReadyState() != webrtc.DataChannelStateOpen || !p.videoBusy.CompareAndSwap(false, true) {
		return false
	}
	data, err := frame.Marshal()
	if err != nil {
		p.videoBusy.Store(false)
		return false
	}
	if _, err = media.ParseFrame(data, profile); err != nil {
		p.videoBusy.Store(false)
		return false
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.videoBusy.Store(false)
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for _, part := range media.Chunks(data, p.sequence.Add(1)) {
			for ch.BufferedAmount() > 0 {
				select {
				case <-p.ctx.Done():
					return
				case <-deadline.C:
					return
				case <-time.After(2 * time.Millisecond):
				}
			}
			if err := ch.Send(part); err != nil {
				return
			}
			select {
			case <-p.ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	return true
}
func (p *Peer) LocalDescription(ctx context.Context, offer bool) (webrtc.SessionDescription, error) {
	var desc webrtc.SessionDescription
	var err error
	if offer {
		desc, err = p.PC.CreateOffer(nil)
	} else {
		desc, err = p.PC.CreateAnswer(nil)
	}
	if err != nil {
		return desc, err
	}
	done := webrtc.GatheringCompletePromise(p.PC)
	if err = p.PC.SetLocalDescription(desc); err != nil {
		return desc, err
	}
	select {
	case <-ctx.Done():
		return desc, ctx.Err()
	case <-done:
		return *p.PC.LocalDescription(), nil
	}
}
func (p *Peer) SetRemote(desc webrtc.SessionDescription) error {
	count := 0
	for _, line := range strings.Split(desc.SDP, "\n") {
		if strings.HasPrefix(line, "m=") {
			if !strings.HasPrefix(line, "m=application ") {
				return errors.New("only data channels are permitted")
			}
			count++
		}
	}
	if count != 1 {
		return errors.New("invalid SDP media sections")
	}
	if _, err := fingerprint(desc.SDP); err != nil {
		return err
	}
	return p.PC.SetRemoteDescription(desc)
}
func fingerprint(sdp string) (string, error) {
	for _, line := range strings.Split(sdp, "\n") {
		if strings.HasPrefix(line, "a=fingerprint:sha-256 ") {
			raw := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(strings.TrimPrefix(line, "a=fingerprint:sha-256 ")), ":", ""))
			value, err := hex.DecodeString(raw)
			if err != nil || len(value) != 32 {
				return "", errors.New("invalid certificate fingerprint")
			}
			return raw, nil
		}
	}
	return "", errors.New("certificate fingerprint missing")
}
func (p *Peer) VerificationCode() (string, error) {
	local := p.PC.LocalDescription()
	remote := p.PC.RemoteDescription()
	if local == nil || remote == nil {
		return "", errors.New("descriptions not ready")
	}
	a, err := fingerprint(local.SDP)
	if err != nil {
		return "", err
	}
	b, err := fingerprint(remote.SDP)
	if err != nil {
		return "", err
	}
	pair := []string{a, b}
	sort.Strings(pair)
	hash := sha256.Sum256([]byte(session.Protocol + "\x00" + strings.Join(pair, "|")))
	raw := hex.EncodeToString(hash[:12])
	groups := []string{}
	for i := 0; i < len(raw); i += 4 {
		groups = append(groups, raw[i:i+4])
	}
	return strings.Join(groups, " "), nil
}
func (p *Peer) Wait(ctx context.Context, event <-chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.Stopped:
		return p.Error()
	case <-event:
		return nil
	}
}
func (p *Peer) Close() error   { p.stop(nil); err := p.PC.Close(); p.wg.Wait(); return err }
func (p *Peer) String() string { return fmt.Sprintf("%s %s", p.Role, p.PC.ConnectionState()) }
