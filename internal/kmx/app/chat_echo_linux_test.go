package app

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestChatWaitSuppressesEchoButPreservesInterrupts(t *testing.T) {
	_, slave := chatPTY(t, 80)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	restore := quietChatWait(slave)
	defer restore()
	during, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if during.Lflag&(unix.ECHO|unix.ECHONL) != 0 || during.Lflag&unix.ISIG != before.Lflag&unix.ISIG || during.Lflag&unix.ICANON != before.Lflag&unix.ICANON {
		t.Fatalf("unexpected terminal flags: before=%x during=%x", before.Lflag, during.Lflag)
	}
	restore()
	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *before != *after {
		t.Fatalf("terminal not restored: %v", err)
	}
}
