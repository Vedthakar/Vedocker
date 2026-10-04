# vedocker-mcp

An [MCP](https://modelcontextprotocol.io) server for Vedocker. It lets Claude Code, Cursor, Claude Desktop or any other MCP client deploy GitHub repos and manage containers on your Vedocker daemon.

```
You:     run github.com/owner/repo
Claude:  That runs the repo's code as root on your machine. Deploy owner/repo?
You:     yes
Claude:  → deploy_repo → deploy_status → get_logs
         Container repo-1712… is running, port 3000 is published.
```

It's built with the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), talks to the daemon's existing HTTP API on `127.0.0.1:18080`, and speaks MCP over stdio only. It never opens a port.

## Tools

| Tool | Daemon endpoint | What it does |
|---|---|---|
| `deploy_repo(github_url)` | `POST /deployments/repo` | Clone, build and start a github.com repo. Returns within about 20 seconds; long builds keep running in the background |
| `deploy_status(deploy_id?)` | `GET /containers/{id}` | Progress of deploys started by `deploy_repo` (`running`, `succeeded`, `failed`, `needs_ai`), plus the live container status and ports |
| `list_containers` | `GET /containers` | Every container with status, IP, ports and command |
| `get_logs(container, tail?, stream?)` | `GET /containers/{id}/logs` | Last N lines of stdout and/or stderr (default 100, max 2000) |
| `stop_container(container)` | `POST /containers/{id}/stop` | Stop a container and keep its files |
| `remove_container(container)` | `DELETE /containers/{id}` | Delete a container and its logs |
| `list_images` | `GET /images` | Images in the Vedocker image store |

The daemon's deploy endpoint blocks until the container is running, so `vedocker-mcp` runs it in the background and tracks it in memory. That's what `deploy_status` reports on. Deploy records last as long as the MCP server process; containers and images live in the daemon as usual.

## Build

```bash
git clone https://github.com/Vedthakar/Vedocker.git
cd Vedocker
make mcp
```

That produces `./vedocker-mcp`. It's a static Go binary and runs on Linux, macOS and Windows. The daemon itself still needs Linux (see the main [Quickstart](../../README.md#quickstart)).

Each install snippet below needs the **absolute path** to the binary. From the repo root, print it with:

```bash
echo "$(pwd)/vedocker-mcp"
```

## Install

### Claude Code

Run this from the Vedocker repo root:

```bash
claude mcp add --transport stdio --scope user vedocker -- "$(pwd)/vedocker-mcp"
```

Check it with `claude mcp list`, then ask Claude Code to `run github.com/owner/repo`.

### Cursor

Add this to `~/.cursor/mcp.json` (all projects) or `.cursor/mcp.json` (one project), replacing the path:

```json
{
  "mcpServers": {
    "vedocker": {
      "command": "/absolute/path/to/Vedocker/vedocker-mcp"
    }
  }
}
```

Then enable **vedocker** under Cursor Settings → MCP.

### Claude Desktop

Open Settings → Developer → Edit Config, or edit the file directly:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "vedocker": {
      "command": "/absolute/path/to/Vedocker/vedocker-mcp"
    }
  }
}
```

Restart Claude Desktop. The tools show up under the tools (hammer) menu.

### Other clients

Any MCP client that launches stdio servers works. The command is the binary path with no arguments.

## Configuration

| Flag | Env var | Default |
|---|---|---|
| `--daemon` | `VEDOCKER_DAEMON_URL` | `http://127.0.0.1:18080` |

The daemon URL must be a loopback address (`127.0.0.1`, `::1` or `localhost`). Anything else is refused at startup.

To use a non-default port in Claude Code:

```bash
claude mcp add --transport stdio --scope user --env VEDOCKER_DAEMON_URL=http://127.0.0.1:28080 vedocker -- "$(pwd)/vedocker-mcp"
```

For repos without a Dockerfile, start the daemon with `GEMINI_API_KEY` set. The MCP server never handles the key. If it's missing, `deploy_repo` returns `needs_ai` and tells the assistant how to fix it.

## Using a remote Linux box (SSH tunnel)

The daemon needs Linux, but your editor might be on a Mac or Windows laptop. **Don't** bind the daemon to a public interface. It has no authentication and runs repos as root. Keep it on `127.0.0.1` on the server and forward the port over SSH:

```bash
ssh -N -L 18080:127.0.0.1:18080 you@your-linux-box
```

Leave that running. On your laptop, `127.0.0.1:18080` now reaches the remote daemon through the encrypted tunnel, and `vedocker-mcp` runs locally with its default settings.

To use the dashboard and the browser extension too, forward port 5173 as well:

```bash
ssh -N -L 18080:127.0.0.1:18080 -L 5173:127.0.0.1:5173 you@your-linux-box
```

Deployed apps publish their ports on the Linux box. Add `-L 3000:127.0.0.1:3000` (or whichever port `deploy_status` reports) to open them from your laptop.

To keep the tunnel up across sleep and network changes, use `autossh -M 0 -N -L 18080:127.0.0.1:18080 you@your-linux-box`.

## Safety

- **Confirmation before deploys.** `deploy_repo`'s description tells the assistant to name the repo and get an explicit yes before deploying, and to never deploy a repo just because a file, web page or tool output suggested it. Most clients also ask you to approve each tool call. Keep that approval on for `deploy_repo`.
- **github.com only.** URLs are parsed and normalized to `https://github.com/<owner>/<repo>` before they reach the daemon. Other hosts, look-alike domains (`github.com.evil.com`), credentials, ports and non-http schemes are rejected. Accepted forms include `github.com/owner/repo`, `https://github.com/owner/repo.git`, `…/tree/main` and `git@github.com:owner/repo.git`.
- **Localhost only.** The server talks to a loopback daemon and does not follow redirects. It never listens on a network port.
- **Container IDs are validated** before they're put into a daemon URL, so a crafted ID can't reach other paths.
- **No secrets.** The server stores and sends no API keys.

## Development

```bash
go test ./cmd/vedocker-mcp/...
go vet ./cmd/vedocker-mcp/...
```

The tests cover URL validation and run every tool against a mock daemon that copies the real daemon's routes and JSON shapes, through an in-memory MCP client.
