package container

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

const linuxCapVersion3 = 0x20080522

type capUserHeader struct {
	Version uint32
	PID     int32
}

type capUserData struct {
	Effective   uint32
	Permitted   uint32
	Inheritable uint32
}

func hardenProcess() error {
	if err := setNoNewPrivs(); err != nil {
		return err
	}
	if err := clearAmbientCaps(); err != nil {
		return err
	}
	if err := dropBoundingCaps(); err != nil {
		return err
	}
	if err := clearAllCaps(); err != nil {
		return err
	}
	return nil
}

func setNoNewPrivs() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("set no_new_privs: %w", err)
	}
	return nil
}

func clearAmbientCaps() error {
	if err := unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0); err != nil {
		return fmt.Errorf("clear ambient capabilities: %w", err)
	}
	return nil
}

func dropBoundingCaps() error {
	for capID := 0; capID <= int(unix.CAP_LAST_CAP); capID++ {
		if err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capID), 0, 0, 0); err != nil {
			return fmt.Errorf("drop bounding capability %d: %w", capID, err)
		}
	}
	return nil
}

func clearAllCaps() error {
	hdr := capUserHeader{
		Version: linuxCapVersion3,
		PID:     0,
	}

	data := [2]capUserData{}

	_, _, errno := unix.RawSyscall(
		unix.SYS_CAPSET,
		uintptr(unsafe.Pointer(&hdr)),
		uintptr(unsafe.Pointer(&data[0])),
		0,
	)
	if errno != 0 {
		return fmt.Errorf("capset clear all capabilities: %w", errno)
	}

	return nil
}

func installSeccompFilter() error {
	const (
		seccompSetModeFilter = 1

		bpfLD   = 0x00
		bpfW    = 0x00
		bpfABS  = 0x20
		bpfJMP  = 0x05
		bpfJEQ  = 0x10
		bpfK    = 0x00
		bpfRET  = 0x06

		seccompRetAllow = 0x7fff0000
		seccompRetErrno = 0x00050000

		seccompDataNR = 0
	)

	denyErrno := uint32(unix.EPERM)

	stmt := func(code uint16, k uint32) unix.SockFilter {
		return unix.SockFilter{
			Code: code,
			Jt:   0,
			Jf:   0,
			K:    k,
		}
	}

	jumpEq := func(k uint32, jt, jf uint8) unix.SockFilter {
		return unix.SockFilter{
			Code: bpfJMP | bpfJEQ | bpfK,
			Jt:   jt,
			Jf:   jf,
			K:    k,
		}
	}

	filters := []unix.SockFilter{
		stmt(bpfLD|bpfW|bpfABS, seccompDataNR),

		jumpEq(unix.SYS_PTRACE, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_KEXEC_LOAD, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_INIT_MODULE, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_FINIT_MODULE, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_DELETE_MODULE, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_OPEN_BY_HANDLE_AT, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_NAME_TO_HANDLE_AT, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_BPF, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_MOUNT, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_UMOUNT2, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		jumpEq(unix.SYS_PIVOT_ROOT, 0, 1),
		stmt(bpfRET|bpfK, seccompRetErrno|denyErrno),

		stmt(bpfRET|bpfK, seccompRetAllow),
	}

	prog := unix.SockFprog{
		Len:    uint16(len(filters)),
		Filter: &filters[0],
	}

	_, _, errno := unix.RawSyscall(
		unix.SYS_SECCOMP,
		uintptr(seccompSetModeFilter),
		0,
		uintptr(unsafe.Pointer(&prog)),
	)
	if errno != 0 {
		return fmt.Errorf("seccomp syscall failed: %w", errno)
	}

	return nil
}