//go:build linux

package verbs

import "syscall"

// testGetsid returns the session id of the calling process. The syscall
// package exports no Getsid on Linux, so call the raw syscall.
func testGetsid() int {
	sid, _, errno := syscall.RawSyscall(syscall.SYS_GETSID, 0, 0, 0)
	if errno != 0 {
		return 0
	}
	return int(sid)
}
