// Command vedocker-mcp is a Model Context Protocol server that lets AI
// assistants (Claude Code, Cursor, Claude Desktop) drive a local Vedocker
// daemon: deploy GitHub repos, list containers and images, read logs, and
// stop or remove containers.
//
// It speaks MCP over stdio only and never opens a network listener. It only
// talks to a daemon on a loopback address.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "0.1.0"

func main() {
	daemonURL := flag.String("daemon", envOr("VEDOCKER_DAEMON_URL", defaultDaemonURL), "Vedocker daemon URL (must be loopback; env VEDOCKER_DAEMON_URL)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("vedocker-mcp", version)
		return
	}

	// stdout carries the MCP protocol, so all logging goes to stderr.
	log.SetOutput(os.Stderr)
	log.SetPrefix("vedocker-mcp: ")

	daemon, err := newDaemonClient(*daemonURL)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := newServer(ctx, serverConfig{daemon: daemon, deployWait: 20 * time.Second})
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
