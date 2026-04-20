package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	case "service":
		if err := serviceCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "service error: %v\n", err)
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
	case "image":
		if err := imageCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "image error: %v\n", err)
			os.Exit(1)
		}
	case "pod":
		if err := podCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "pod error: %v\n", err)
			os.Exit(1)
		}
	case "deploy":
		if err := deployCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "deploy error: %v\n", err)
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
		return fmt.Errorf("usage: minicontainer run [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs-or-image> <command> [args...]")
	}

	rootfs, err := container.ResolveRootfs(rest[0])
	if err != nil {
		return err
	}

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
		return fmt.Errorf("usage: minicontainer create <id> [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs-or-image> <command> [args...]")
	}

	id := args[0]
	opts, rest, err := parseOptions(args[1:])
	if err != nil {
		return err
	}
	if len(rest) < 2 {
		return fmt.Errorf("usage: minicontainer create <id> [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs-or-image> <command> [args...]")
	}

	rootfs, err := container.ResolveRootfs(rest[0])
	if err != nil {
		return err
	}

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

func imageCmd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: minicontainer image <add|ls|inspect|rm|import|export|pull|build> ...")
	}

	switch args[0] {
	case "add":
		if len(args) != 3 {
			return fmt.Errorf("usage: minicontainer image add <name[:tag]> <rootfs-path>")
		}
		return container.AddImage(args[1], args[2])
	case "ls":
		if len(args) != 1 {
			return fmt.Errorf("usage: minicontainer image ls")
		}

		images, err := container.ListImages()
		if err != nil {
			return err
		}

		for _, img := range images {
			fmt.Printf("%s\t%s\t%s\t%s\n", img.Ref, img.Source, img.Rootfs, img.CreatedAt)
		}
		return nil
	case "inspect":
		if len(args) != 2 {
			return fmt.Errorf("usage: minicontainer image inspect <name[:tag]>")
		}

		img, err := container.GetImage(args[1])
		if err != nil {
			return err
		}

		data, err := json.MarshalIndent(img, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	case "rm":
		if len(args) != 2 {
			return fmt.Errorf("usage: minicontainer image rm <name[:tag]>")
		}
		return container.RemoveImage(args[1])
	case "import":
		if len(args) != 3 {
			return fmt.Errorf("usage: minicontainer image import <name[:tag]> <rootfs-tar-path>")
		}
		return container.ImportImage(args[1], args[2])
	case "export":
		if len(args) != 3 {
			return fmt.Errorf("usage: minicontainer image export <name[:tag]> <output-tar-path>")
		}
		return container.ExportImage(args[1], args[2])
	case "pull":
		if len(args) != 2 {
			return fmt.Errorf("usage: minicontainer image pull <name[:tag]>")
		}
		return container.PullImage(args[1])
	case "build":
		return imageBuildCmd(args[1:])
	default:
		return fmt.Errorf("unknown image subcommand: %s", args[0])
	}
}

func imageBuildCmd(args []string) error {
	var tag string
	var dockerfile string
	var context string

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-t":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value after -t")
			}
			tag = args[i+1]
			i += 2
		case "-f":
			if i+1 >= len(args) {
				return fmt.Errorf("missing value after -f")
			}
			dockerfile = args[i+1]
			i += 2
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown build option: %s", args[i])
			}
			if context != "" {
				return fmt.Errorf("image build accepts exactly one context directory")
			}
			context = args[i]
			i++
		}
	}

	if tag == "" || dockerfile == "" || context == "" {
		return fmt.Errorf("usage: minicontainer image build -t <name[:tag]> -f <Dockerfile-path> <context-dir>")
	}

	return container.BuildImage(tag, dockerfile, context)
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
  minicontainer run [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs-or-image> <command> [args...]
  minicontainer create <id> [-e KEY=value]... [-v /host:/container]... [-p HOST_PORT:CONTAINER_PORT]... <rootfs-or-image> <command> [args...]
  minicontainer start <id>
  minicontainer stop <id>
  minicontainer rm <id>
  minicontainer logs <id> [stdout|stderr]
  minicontainer exec <id> <command> [args...]
  minicontainer image <add|ls|inspect|rm|import|export|pull|build> ...
  minicontainer pod apply -f <file>
  minicontainer pod delete <name>
  minicontainer pod get
  minicontainer deploy apply -f <file>
  minicontainer deploy get
  minicontainer deploy delete <name>
  minicontainer service apply -f <file>
  minicontainer service get
  minicontainer service delete <name>
`)
}
