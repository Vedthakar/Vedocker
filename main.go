package main

import (
	"fmt"
	"os"

	"github.com/vthecar/minicontainer/pkg/container"
)

func main() {
	if len(os.Args) < 2 {
		help()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if err := run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run error: %v\n", err)
			os.Exit(1)
		}
	case "child":
		if err := child(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "child error: %v\n", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		help()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		help()
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: minicontainer run <rootfs> <command> [args...]")
	}

	rootfs := args[0]
	cmd := args[1:]
	return container.Run(rootfs, cmd)
}

func child(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: minicontainer child <rootfs> <command> [args...]")
	}

	rootfs := args[0]
	cmd := args[1:]
	return container.Child(rootfs, cmd)
}

func help() {
	fmt.Println(`minicontainer - tiny educational container runtime (Phase 1)

Usage:
  minicontainer run <rootfs> <command> [args...]
  minicontainer help

Examples:
  sudo ./minicontainer run /var/lib/minicontainer/images/alpine /bin/sh
  sudo ./minicontainer run /var/lib/minicontainer/images/alpine /bin/echo hello

Notes:
  - "run" re-execs the current binary as "child" in new namespaces
  - rootfs should be a prepared Linux root filesystem
  - Phase 1 focuses on namespace + mount/chroot basics`)
}
