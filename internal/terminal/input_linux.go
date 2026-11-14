package terminal

import (
	"golang.org/x/sys/unix"
	"golang.org/x/term"
	"os"
)

func suspendInputEcho() func() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return func() {}
	}
	original, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return func() {}
	}
	quiet := *original
	quiet.Lflag &^= unix.ECHO | unix.ECHONL
	if unix.IoctlSetTermios(fd, unix.TCSETS, &quiet) != nil {
		return func() {}
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, original) }
}
