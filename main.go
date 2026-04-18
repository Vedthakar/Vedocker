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
	case "create":
		if err := create(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "create error: %v\n", err)
			os.Exit(1)
		}
	case "start":
		if err := start(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "start error: %v\n", err)
			os.Exit(1)
		}
	case "stop":
		if err := stop(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "stop error: %v\n", err)
			os.Exit(1)
		}
	case "rm":
		if err := rm(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "rm error: %v\n", err)
			os.Exit(1)
		}
	case "logs":
		if err := logs(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "logs error: %v\n", err)
			os.Exit(1)
		}
	case "exec":
		if err := execInContainer(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "exec error: %v\n", err)
			os.Exit(1)
		}
	case "exec-child":
		if err := execChild(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "exec-child error: %v\n", err)
			os.Exit(1)
		}
	case "exec-stage2":
		if err := execStage2(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "exec-stage2 error: %v\n", err)
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
	opts, rest, err := parseOptions(args)
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: minicontainer run [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs> <command> [args...]")
	}

	rootfs := rest[0]
	cmd := rest[1:]
	return container.Run(rootfs, cmd, opts.Env, opts.Mounts, opts.Ports)
}

func child(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: minicontainer child <rootfs> <command> [args...]")
	}

	rootfs := args[0]
	cmd := args[1:]
	return container.Child(rootfs, cmd)
}

func create(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: minicontainer create <id> [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs> <command> [args...]")
	}

	id := args[0]
	opts, rest, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: minicontainer create <id> [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs> <command> [args...]")
	}

	rootfs := rest[0]
	cmd := rest[1:]
	return container.Create(id, rootfs, cmd, opts.Env, opts.Mounts, opts.Ports)
}

func start(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer start <id>")
	}

	return container.Start(args[0])
}

func stop(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer stop <id>")
	}

	return container.Stop(args[0])
}

func rm(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: minicontainer rm <id>")
	}

	return container.Remove(args[0])
}

func logs(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: minicontainer logs <id> [stdout|stderr]")
	}

	stream := "stdout"
	if len(args) == 2 {
		stream = args[1]
	}

	return container.Logs(args[0], stream)
}

func execInContainer(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: minicontainer exec <id> <command> [args...]")
	}

	id := args[0]
	cmd := args[1:]
	return container.Exec(id, cmd)
}

func execChild(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: minicontainer exec-child <id> <command> [args...]")
	}

	id := args[0]
	cmd := args[1:]
	return container.ExecChild(id, cmd)
}

func execStage2(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: minicontainer exec-stage2 <rootfs> <command> [args...]")
	}

	rootfs := args[0]
	cmd := args[1:]
	return container.ExecStage2(rootfs, cmd)
}

type cliOptions struct {
	Env    []string
	Mounts []container.Mount
	Ports  []container.Port
}

func parseOptions(args []string) (cliOptions, []string, error) {
	var opts cliOptions
	i := 0

	for i < len(args) {
		switch args[i] {
		case "--":
			return opts, args[i+1:], nil
		case "-e":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("missing value after -e")
			}
			opts.Env = append(opts.Env, args[i+1])
			i += 2
		case "-v":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("missing value after -v")
			}
			m, err := container.ParseMountSpec(args[i+1])
			if err != nil {
				return opts, nil, err
			}
			opts.Mounts = append(opts.Mounts, m)
			i += 2
		case "-p":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("missing value after -p")
			}
			p, err := container.ParsePortSpec(args[i+1])
			if err != nil {
				return opts, nil, err
			}
			opts.Ports = append(opts.Ports, p)
			i += 2
		default:
			if len(args[i]) > 0 && args[i][0] == '-' {
				return opts, nil, fmt.Errorf("unknown option: %s", args[i])
			}
			return opts, args[i:], nil
		}
	}

	return opts, nil, nil
}

func help() {
	fmt.Println(`minicontainer - tiny educational container runtime

Usage:
  minicontainer run [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs> <command> [args...]
  minicontainer create <id> [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs> <command> [args...]
  minicontainer start <id>
  minicontainer stop <id>
  minicontainer rm <id>
  minicontainer logs <id> [stdout|stderr]
  minicontainer exec <id> <command> [args...]
  minicontainer help

Examples:
  sudo ./minicontainer run -e NAME=ved -v /tmp:/hosttmp -p 8080:80 /var/lib/minicontainer/images/alpine /bin/sh
  sudo ./minicontainer create demo -e NAME=ved -v /tmp:/hosttmp -p 8080:80 /var/lib/minicontainer/images/alpine /bin/sh
  sudo ./minicontainer start demo
  sudo ./minicontainer exec demo /bin/sh
  sudo ./minicontainer logs demo
  sudo ./minicontainer stop demo
  sudo ./minicontainer rm demo`)
}