//go:build !windows

package terminal

import (
	"bytes"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func query(request string) []byte {
	fd, err := unix.Open("/dev/tty", unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer unix.Close(fd)
	original, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	defer term.Restore(fd, original)
	unix.Write(fd, []byte(esc+"[?1049h"+esc+"[?25l"+esc+"[2J"+request+esc+"[c"))
	defer unix.Write(fd, []byte(esc+"[?25h"+esc+"[?1049l"))
	response := []byte{}
	deadline := time.Now().Add(700 * time.Millisecond)
	for len(response) < 4096 && time.Now().Before(deadline) {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, int(time.Until(deadline).Milliseconds()))
		if err != nil || ready == 0 {
			break
		}
		buffer := make([]byte, 4096-len(response))
		n, err := unix.Read(fd, buffer)
		if err != nil || n == 0 {
			break
		}
		response = append(response, buffer[:n]...)
		if bytes.Contains(response, []byte(esc+"[?")) && bytes.HasSuffix(response, []byte("c")) {
			break
		}
	}
	_ = os.Stdout.Sync()
	return response
}
