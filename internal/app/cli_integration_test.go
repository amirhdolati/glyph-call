package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hinshun/vt10x"
)

type captureOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *captureOutput) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(data)
}
func (c *captureOutput) String() string { c.mu.Lock(); defer c.mu.Unlock(); return c.b.String() }

func TestTwoCompiledGoClientsCompleteCodeSettingsConsentAndHangup(t *testing.T) {
	for _, mode := range []string{"hidden", "fun"} {
		t.Run(mode, func(t *testing.T) { testCompiledClients(t, mode) })
	}
}

// Parse the actual rendered terminal output, rather than merely checking that
// the connection was established. The call uses generated frames in both directions.
func funPanesVisible(output string) bool {
	end := strings.LastIndex(output, "\x1b[?2026l")
	if end < 0 {
		return false
	}
	start := strings.LastIndex(output[:end], "\x1b[?2026h")
	if start < 0 {
		return false
	}
	vt := vt10x.New(vt10x.WithSize(100, 30))
	vt.Write([]byte(output[start:end]))
	counts := [2]int{}
	for y := 0; y < 30; y++ {
		for x := 0; x < 100; x++ {
			r := vt.Cell(x, y).Char
			if r < 0x2801 || r > 0x28ff {
				continue
			}
			if y < 2 || y >= 27 {
				return false
			}
			if x >= 1 && x < 48 {
				counts[0]++
			} else if x >= 51 && x < 98 {
				counts[1]++
			} else {
				return false
			}
		}
	}
	return counts[0] > 40 && counts[1] > 40 && vt.Cell(1, 0).Char == 'Y' && vt.Cell(51, 0).Char == 'P'
}

func testCompiledClients(t *testing.T, mode string) {
	binary, relay := os.Getenv("ASCII_CALL_TEST_BINARY"), os.Getenv("ASCII_CALL_TEST_RELAY")
	if binary == "" || relay == "" {
		t.Skip("set ASCII_CALL_TEST_BINARY and ASCII_CALL_TEST_RELAY for native CLI integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	start := func(args ...string) (*exec.Cmd, io.WriteCloser, *captureOutput) {
		cmd := exec.CommandContext(ctx, binary, args...)
		output := &captureOutput{}
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.Env = append(os.Environ(), "TERM_PROGRAM=Apple_Terminal", "TERM=xterm-256color", "LANG=en_US.UTF-8")
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				cmd.Process.Kill()
				cmd.Wait()
			}
		})
		return cmd, input, output
	}
	host, hostInput, hostOutput := start("host", "--mode", mode, "--synthetic", "--voice", "robot", "--no-audio", "--no-camera", "--no-stun", "--relay-url", relay)
	wait := func(condition func() bool) {
		deadline := time.Now().Add(20 * time.Second)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatalf("native CLI timed out; host output: %s", hostOutput.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	codePattern := regexp.MustCompile(`Share privately: ([0-9]+-[A-Za-z0-9-]+)`)
	wait(func() bool { return codePattern.MatchString(hostOutput.String()) })
	code := codePattern.FindStringSubmatch(hostOutput.String())[1]
	guest, guestInput, guestOutput := start("connect", "--synthetic", "--no-audio", "--no-camera", "--no-stun", "--relay-url", relay, code)
	wait(func() bool {
		return strings.Contains(hostOutput.String(), "Codes match?") && strings.Contains(guestOutput.String(), "Codes match?")
	})
	verifier := regexp.MustCompile(`[0-9a-f]{4}(?: [0-9a-f]{4}){5}`)
	if verifier.FindString(hostOutput.String()) != verifier.FindString(guestOutput.String()) {
		t.Fatal("CLI certificate comparison codes differ")
	}
	for _, output := range []*captureOutput{hostOutput, guestOutput} {
		if !strings.Contains(output.String(), "host: "+mode+"/") || !strings.Contains(output.String(), "guest: "+mode+"/") {
			t.Fatal("host mode not applied to both clients")
		}
	}
	io.WriteString(hostInput, "yes\n")
	io.WriteString(guestInput, "yes\n")
	wait(func() bool {
		return strings.Contains(hostOutput.String(), "DTLS encrypted") && strings.Contains(guestOutput.String(), "DTLS encrypted")
	})
	if mode == "fun" {
		wait(func() bool { return funPanesVisible(hostOutput.String()) && funPanesVisible(guestOutput.String()) })
		first := hostOutput.String()
		wait(func() bool {
			return strings.Count(hostOutput.String(), "\x1b[?2026l") >= strings.Count(first, "\x1b[?2026l")+3
		})
		if !funPanesVisible(hostOutput.String()) || !funPanesVisible(guestOutput.String()) {
			t.Fatal("panes corrupted after live updates")
		}
	}
	host.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- host.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("host did not exit cleanly")
	}
	guestDone := make(chan error, 1)
	go func() { guestDone <- guest.Wait() }()
	select {
	case err := <-guestDone:
		if err != nil {
			t.Fatal(err, guestOutput.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("guest did not notice hangup")
	}
	if !strings.Contains(hostOutput.String(), "\x1b[?1049l") || !strings.Contains(guestOutput.String(), "\x1b[?1049l") {
		t.Fatal("terminal was not restored on hangup")
	}
}
