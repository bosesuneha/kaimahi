package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// Keep canonical input and ISIG so Ctrl-C still interrupts the active request,
// but prevent keys pressed during inference from being echoed into the reply.
func quietChatWait(in *os.File) func() {
	if in == nil {
		return func() {}
	}
	fd := int(in.Fd())
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return func() {}
	}
	quiet := *state
	quiet.Lflag &^= unix.ECHO | unix.ECHONL
	if unix.IoctlSetTermios(fd, unix.TCSETS, &quiet) != nil {
		return func() {}
	}
	return func() { _ = unix.IoctlSetTermios(fd, unix.TCSETS, state) }
}
