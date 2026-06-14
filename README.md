<div align="center">

<img src="https://raw.githubusercontent.com/Vedthakar/Vedocker/main/assets/cover.png" alt="Vedocker — Modern Docker Visualization" width="100%" />

<br />

# Vedocker

**A Docker and Kubernetes platform built from scratch in Go and Linux — with AI that can containerize any GitHub repo, even if it has no Dockerfile.**

<br />

[![Language: Go](https://img.shields.io/badge/Language-Go-00ADD8?style=for-the-badge&logo=go)](https://golang.org)
[![Platform: Linux](https://img.shields.io/badge/Platform-Linux-FCC624?style=for-the-badge&logo=linux&logoColor=black)](https://kernel.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue?style=for-the-badge)](LICENSE)
[![Built from Scratch](https://img.shields.io/badge/Built-From%20Scratch-red?style=for-the-badge)]()

</div>

---

## What is Vedocker?

Vedocker is a full container platform I built from the ground up — no Docker Engine, no containerd, no Kubernetes underneath. Every layer was implemented directly in Go using Linux kernel primitives: namespaces, cgroups, root filesystems, process isolation, networking, and iptables-based port publishing.

On top of that engine I built a complete platform: image management, a Dockerfile-style build system, AI-assisted repo deployment, a live web dashboard, and a Kubernetes-style orchestration layer with Pods, Deployments, scaling, reconcile logic, and background self-healing.

**The core problem Vedocker solves:**

> How do you take any raw GitHub repository — even one with no Dockerfile, no setup instructions, and no documentation — and turn it into a live running application in one step?

Paste in a GitHub URL. Vedocker inspects the repo, detects whether a Dockerfile exists, builds and runs it if one is present, and if not — uses AI to generate one, validate it, and continue through the build pipeline automatically.

---

## Why This Matters

Most developers have used Docker. Very few have built a Docker-style runtime from scratch.  
Most developers have heard of Kubernetes. Very few have built a reconcile loop on top of their own container engine.

Vedocker is not a wrapper. It is not a thin CLI over existing infrastructure tooling. The container runtime, image store, build engine, networking layer, daemon API, and orchestration control plane were all implemented manually, from first principles.

| What most developers do | What Vedocker does |
|------------------------|--------------------|
| `docker run` a container | Implements `create`, `start`, `stop`, `exec`, `logs` using Linux primitives |
| `docker build` an image | Parses Dockerfile instructions, mutates filesystems, registers images |
| `kubectl apply` a deployment | Writes desired state, creates replicas, runs reconcile loops, self-heals |
| Paste a Dockerfile into CI | Generates the Dockerfile with AI when one doesn't exist |

---

## Feature Overview

### 1. From-Scratch Container Runtime

The heart of Vedocker. Built in Go using Linux kernel APIs — not Docker.

**Supported operations:**

| Command | Description |
|---------|-------------|
| `create` | Allocate a new container with isolated namespaces and cgroups |
| `start` | Launch the container process in its isolated environment |
| `stop` | Gracefully terminate the container and clean up |
| `rm` | Remove container state and metadata |
| `logs` | Stream stdout/stderr from a running or stopped container |
| `exec` | Run a command inside a live container |

**Under the hood:**
- **PID namespaces** — container processes can't see host processes
- **Mount namespaces** — isolated root filesystem per container
- **Network namespaces** — separate network stack per container
- **cgroups v1/v2** — CPU and memory resource limits
- **iptables** — port publishing from host to container

---

### 2. Custom Image System

Vedocker manages its own image store and metadata — no Docker daemon or registry client required.

| Operation | What it does |
|-----------|-------------|
| `image pull` | Fetch a base image (e.g. `alpine:latest`) and register locally |
| `image build` | Build an image from a Dockerfile against the local image store |
| `image ls` | List all registered images with ID, name, tag, and size |
| `image inspect` | Show full metadata for an image |
| `image import` | Import a tarball as an image |
| `image export` | Export an image to a tarball |
| `image rm` | Remove an image from the local store |

---

### 3. Dockerfile-Style Build Engine

A real build system that parses Dockerfile instructions and turns source code into runnable images.

**Supported instructions:**

```dockerfile
FROM alpine:latest
WORKDIR /app
COPY . .
RUN apk add --no-cache nodejs npm
ENV NODE_ENV=production
EXPOSE 3000
CMD ["node", "server.js"]
```

Also supports: multiline `RUN`, glob patterns in `COPY`, and automatic base image pulling when a `FROM` image isn't cached locally.

---

### 4. AI-Assisted GitHub Repo Deployment

The killer feature. Paste any GitHub URL — Vedocker does the rest.

```
                                          ┌─────────────────────┐
  User pastes GitHub URL                  │  Dockerfile exists?  │
           │                              └──────────┬──────────┘
           ▼                                         │
  Clone repository                         Yes ──────┼────── No
           │                                         │           │
           ▼                                         ▼           ▼
  Inspect repo structure               Use existing  AI analyzes repo
           │                           Dockerfile    structure + stack
           │                               │             │
           └───────────────────────────────┘             │
                           │                       Generate Dockerfile
                           ▼                       Validate output
                     Build image                         │
                           │                             │
                           └─────────────────────────────┘
                                         │
                                         ▼
                               Start container in runtime
                                         │
                                         ▼
                           Expose logs + controls in UI
```

The AI fallback inspects the repository structure, infers the language and framework, generates a working Dockerfile, validates it, and continues directly into the normal build pipeline. No manual intervention required.

---

### 5. Live Web Dashboard

A full frontend dashboard (Vite + Go daemon API) that makes the platform feel like a real product.

**Docker view:**
- Image list, inspect, pull, remove
- Container list, create, start, stop, remove
- Live log streaming
- GitHub repo deploy with real-time build output

**Kubernetes view:**
- Pod list, apply, delete
- Deployment list, apply, delete, scale
- Live cluster state
- Reconcile and self-heal controls

---

### 6. Kubernetes-Style Orchestration

After building the container runtime, I built a control plane on top of it.

**Pod management:**
```bash
./minicontainer pod apply hello-pod.yaml
./minicontainer pod ls
./minicontainer pod delete hello-pod
```

**Deployment management:**
```bash
./minicontainer deploy apply webdeploy.yaml
./minicontainer deploy ls
./minicontainer deploy scale my-app 3
./minicontainer deploy reconcile my-app
./minicontainer deploy reconcile-all
```

**How reconciliation works:**
1. Desired state is written to local storage when you apply a Deployment
2. The reconcile loop compares desired replicas to running replicas
3. Missing replicas are created; extra replicas are terminated
4. A background loop runs continuously — if a container dies, Vedocker restarts it automatically
5. The Kubernetes dashboard reflects live cluster state at all times

This is genuine desired-state management, not manual container management with extra steps.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 8: Frontend (Vite dashboard)                             │
│  Docker view • Kubernetes view • Repo deploy UI • Log stream    │
└─────────────────────────────────┬───────────────────────────────┘
                                  │ HTTP
┌─────────────────────────────────▼───────────────────────────────┐
│  Layer 5: Daemon + API (minicontainerd)                         │
│  Container routes • Image routes • Orchestration endpoints      │
│  Repo deploy flow • AI pipeline trigger • Kubernetes data       │
└──────┬──────────┬──────────┬──────────┬────────────┬────────────┘
       │          │          │          │            │
┌──────▼──┐ ┌────▼────┐ ┌───▼───┐ ┌────▼────┐ ┌────▼──────────┐
│Layer 1  │ │Layer 3  │ │Layer 4│ │Layer 6  │ │Layer 7        │
│Runtime  │ │Images   │ │Builder│ │AI Deploy│ │Orchestration  │
│         │ │         │ │       │ │Pipeline │ │               │
│create   │ │pull     │ │FROM   │ │clone    │ │Pod apply      │
│start    │ │build    │ │COPY   │ │inspect  │ │Deployment     │
│stop     │ │ls       │ │RUN    │ │detect   │ │Replica mgmt   │
│exec     │ │inspect  │ │ENV    │ │AI gen   │ │Reconcile loop │
│logs     │ │import   │ │CMD    │ │validate │ │Self-healing   │
│rm       │ │export   │ │EXPOSE │ │build    │ │               │
└──────┬──┘ └─────────┘ └───────┘ └─────────┘ └───────────────┘
       │
┌──────▼──────────────────────────────────┐
│  Layer 2: Networking                    │
│  Network namespaces • IP allocation     │
│  iptables port publishing • NAT/egress  │
└─────────────────────────────────────────┘
```

**Package layout:**

```
minicontainer/
├── main.go                  ← CLI entry point
├── minicontainerd/          ← Daemon binary
├── minicontainer-ui/        ← Vite frontend dashboard
├── cmd/                     ← CLI command handlers
├── pkg/
│   ├── container/           ← Runtime: create, start, stop, exec, logs
│   ├── cgroups/             ← Resource limits (CPU, memory)
│   ├── network/             ← Namespace, IP, iptables
│   ├── image/               ← Image store and metadata
│   ├── deploy/              ← Deployment desired-state management
│   └── minicontainerd/      ← Daemon HTTP server and routes
├── deployment.go            ← Deployment controller
├── pod.go                   ← Pod controller
├── service.go               ← Service object
└── scripts/
    └── bootstrap.sh         ← Environment setup script
```

---

## Getting Started

### Prerequisites

- Linux (Ubuntu 20.04+ recommended)
- Go 1.25+
- Root/sudo access (required for namespaces, cgroups, iptables)
- Node.js 18+ (for the web UI only)

### Build

```bash
git clone https://github.com/Vedthakar/Vedocker.git
cd Vedocker

# Build everything
make build

# Or build individually
go build -o minicontainer .
go build -o minicontainerd ./pkg/minicontainerd/
```

### Run the daemon

```bash
# Start the background daemon (required for UI and orchestration)
sudo ./minicontainerd
```

### Run the web UI

```bash
cd minicontainer-ui
npm install
npm run dev
# Open http://localhost:5173
```

### Bootstrap the environment (optional)

```bash
sudo ./scripts/bootstrap.sh
```

---

## Example Commands

### Container and image operations

```bash
# Pull a base image
sudo ./minicontainer image pull alpine:latest

# Build an image from a Dockerfile
sudo ./minicontainer image build -t my-app:v1 -f Dockerfile .

# List images
sudo ./minicontainer image ls

# Create and start a container
sudo ./minicontainer create my-container my-app:v1 /bin/sh -c "sleep 3600"
sudo ./minicontainer start my-container

# View logs
sudo ./minicontainer logs my-container

# Execute a command inside the container
sudo ./minicontainer exec my-container /bin/sh

# Stop and remove
sudo ./minicontainer stop my-container
sudo ./minicontainer rm my-container
```

### GitHub repo deployment

```bash
# Deploy any GitHub repo (AI generates Dockerfile if missing)
sudo ./minicontainer deploy-repo https://github.com/user/repo
```

### Kubernetes-style orchestration

```bash
# Apply a Pod
sudo ./minicontainer pod apply hello-pod.yaml

# Apply a Deployment with replicas
sudo ./minicontainer deploy apply webdeploy.yaml

# Scale a deployment
sudo ./minicontainer deploy scale my-app 3

# Reconcile (repair missing replicas)
sudo ./minicontainer deploy reconcile my-app

# Reconcile everything
sudo ./minicontainer deploy reconcile-all

# List running pods and deployments
sudo ./minicontainer pod ls
sudo ./minicontainer deploy ls
```

### Example Deployment YAML

```yaml
# webdeploy.yaml
name: my-web-app
image: my-app:v1
replicas: 3
ports:
  - host: 8080
    container: 3000
env:
  NODE_ENV: production
```

---

## System Design Flows

### Docker-style flow

```
User input (image or GitHub URL)
  → Daemon receives request
  → Image resolved or built from source
  → Container created with isolated namespace + cgroup
  → Process launched inside rootfs
  → Port published via iptables
  → Logs streamed to UI
```

### AI repo deploy flow

```
User pastes GitHub URL
  → Daemon clones repository
  → Inspect: does a Dockerfile exist?
  ├── Yes → proceed to build
  └── No  → AI analyzes repo structure
              → Infer language + framework
              → Generate Dockerfile
              → Validate output
              → Proceed to build
  → Build image
  → Start container
  → UI shows status, logs, controls
```

### Kubernetes reconcile flow

```
User applies Deployment YAML
  → Desired state written to storage (replicas: 3)
  → Reconcile loop runs
  → Actual state checked (running containers)
  → Delta calculated (need 3, have 0 → create 3)
  → Replicas created on the custom runtime
  → Background loop watches continuously
  → Container dies → loop detects → replica recreated
```

---

## What This Demonstrates

For anyone reading this as a portfolio piece:

| Skill Area | What Vedocker shows |
|-----------|---------------------|
| **Systems programming** | Go + Linux kernel APIs — namespaces, cgroups, proc lifecycle |
| **Networking** | Network namespaces, IP allocation, iptables NAT, port publishing |
| **Infrastructure design** | Layered platform architecture from runtime to control plane |
| **Orchestration** | Desired-state management, reconcile loops, self-healing |
| **AI integration** | LLM-powered Dockerfile generation as a practical product fallback |
| **Full-stack** | Go backend daemon + Vite frontend + REST API |
| **Product thinking** | Solving a real problem (any repo → running app) through systems engineering |

---

## Current Status

### Fully working

| Feature | Status |
|---------|--------|
| From-scratch container runtime | ✅ |
| Custom image store (pull, build, import, export) | ✅ |
| Dockerfile-lite build engine | ✅ |
| GitHub repo deployment | ✅ |
| AI-generated Dockerfile fallback | ✅ |
| Daemon + Docker-style web UI | ✅ |
| Pod management | ✅ |
| Deployment management + scaling | ✅ |
| Background reconcile loop + self-healing | ✅ |
| Kubernetes dashboard | ✅ |
| Service object + state management | ✅ |

### Known limitation

Fully robust multi-replica Service proxying and load balancing is deferred — it requires a deeper redesign of the runtime networking model. This is an intentional boundary, not hidden incomplete behavior.

---

## Built by

[Ved Thakar](https://github.com/Vedthakar)
