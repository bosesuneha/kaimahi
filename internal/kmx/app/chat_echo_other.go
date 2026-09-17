//go:build !linux

package app

import "os"

func quietChatWait(_ *os.File) func() { return func() {} }
