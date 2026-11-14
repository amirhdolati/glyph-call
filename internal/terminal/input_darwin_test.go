package terminal

import (
	"bytes"
	"os"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func TestDisplaySuppressesInputEchoAndRestoresTerminal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	fd := int(slave.Fd())
	before, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		t.Fatal(err)
	}
	before.Lflag |= unix.ECHO | unix.ECHONL | unix.ISIG
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, before); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = slave
	defer func() { os.Stdin = previous }()
	d := Display{Out: &bytes.Buffer{}}
	d.Start()
	defer d.Close()
	during, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil || during.Lflag&(unix.ECHO|unix.ECHONL) != 0 || during.Lflag&unix.ISIG == 0 {
		t.Fatalf("input can scroll display, or Ctrl+C disabled: %v, %v", during, err)
	}
	d.Close()
	after, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil || *before != *after {
		t.Fatal("input settings were not restored", err)
	}
}
