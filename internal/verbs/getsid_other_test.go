//go:build !linux

package verbs

import "syscall"

// testGetsid returns the session id of the calling process.
func testGetsid() int {
	sid, _ := syscall.Getsid(0)
	return sid
}
