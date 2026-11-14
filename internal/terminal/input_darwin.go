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
	original, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return func() {}
	}
	quiet := *original
	quiet.Lflag &^= unix.ECHO | unix.ECHONL
	if unix.IoctlSetTermios(fd, unix.TIOCSETA, &quiet) != nil {
		return func() {}
	}
	// Preserve ISIG so Ctrl+C still works, and restore the user's input settings.
	return func() { _ = unix.IoctlSetTermios(fd, unix.TIOCSETA, original) }
}
