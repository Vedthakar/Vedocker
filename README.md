# Vedocker

### An AI-powered container platform built from scratch in Go and Linux that can build, run, and orchestrate applications from a GitHub repo, even when no Dockerfile exists.

Vedocker is a full infrastructure platform I built from the ground up by implementing the core mechanics of containerization directly in the Linux terminal.

Instead of relying on Docker Engine, containerd, or Kubernetes under the hood, I built the underlying engine myself using Linux primitives such as namespaces, cgroups, root filesystems, process isolation, lifecycle control, networking, and port publishing. On top of that engine, I built a complete platform with image management, a Dockerfile-style builder, AI-assisted repo deployment, a daemon and web UI, and a Kubernetes-style orchestration layer with Pods, Deployments, scaling, reconcile logic, and background self-healing.

Vedocker is both:
- a real product for quickly previewing, running, and managing codebases
- a deep systems project that rebuilds key ideas behind Docker and Kubernetes from first principles

---

## What is Vedocker?

Vedocker is designed to solve a simple but frustrating problem:

**how do you take a raw GitHub repository and turn it into a live, runnable application with as little friction as possible?**

In the real world, that process is often messy:
- some repositories already have a Dockerfile
- some have incomplete setup instructions
- some work only on the original developer’s machine
- some require a lot of trial and error before they run at all

Vedocker handles that path in one system.

A user can paste in a GitHub repository, and Vedocker can:
- inspect the repository
- detect whether a Dockerfile already exists
- build and run it if one exists
- generate a Dockerfile with AI if one does not
- continue through the build pipeline
- start the app in an isolated container
- expose logs, runtime state, and management controls through a dashboard

That makes Vedocker a practical developer tool, not just a systems experiment.

---

## Why this project stands out

A lot of developers use Docker.  
Very few build a Docker-style runtime themselves.

A lot of developers use Kubernetes.  
Very few build orchestration logic on top of their own container engine.

What makes Vedocker different is that it is not a wrapper around existing infrastructure tools. The core container engine was implemented directly in Go and Linux by working through the low-level systems behavior that modern container platforms depend on.

That includes:
- Linux namespaces
- cgroups
- process lifecycle handling
- root filesystem isolation
- container networking
- iptables-based port publishing
- image storage and metadata
- build execution
- daemon APIs
- desired-state orchestration
- reconcile loops
- background self-healing

So while Vedocker works as a product, it also demonstrates deep systems engineering across operating systems, infrastructure, networking, and orchestration.

---

## Core product idea

Vedocker has two major layers:

### 1. A Docker-style container engine
A complete engine for:
- building images
- running containers
- managing lifecycle
- handling logs and exec
- exposing apps through published ports
- deploying repositories from GitHub

### 2. A Kubernetes-style orchestration layer
A higher-level control plane for:
- Pods
- Deployments
- replica management
- scaling
- reconcile behavior
- background self-healing
- live cluster state in the UI

This means Vedocker is not just a runtime and not just a dashboard.

It is a full platform.

---

## Key product use cases

### 1. Run a GitHub repo without fighting setup
A user finds a repository and wants to see what it does.

Normally, that means reading documentation, installing dependencies, figuring out runtime versions, writing environment setup, and hoping the app still works.

With Vedocker, the user can paste in the repo and let the platform:
- inspect the project
- detect or generate a Dockerfile
- build the image
- start the container
- show logs and runtime state in the UI

That shortens the path from “interesting repo” to “working app.”

---

### 2. Solve the classic “works on my machine” problem
One developer says the app works locally. Another person cannot reproduce the environment.

Vedocker helps bridge that gap by converting the repository into a portable containerized environment. If packaging is missing, the AI fallback can help generate the missing setup. Once the project runs, it becomes much easier to reproduce, inspect, and share.

This is useful for:
- teammates
- class projects
- hackathon demos
- portfolio apps
- internal tools
- quick reviews

---

### 3. Preview and test unfamiliar apps faster
Vedocker acts as a lightweight local preview platform.

Instead of manually piecing together commands, environments, and logs, a user can deploy a repo and manage it through one system. That makes it useful for quickly testing unfamiliar codebases and exploring projects without spending time on manual setup.

---

### 4. Use AI to bridge incomplete repositories
Many repositories contain application code but do not cleanly describe how to containerize or run themselves.

Vedocker addresses that directly.

If a repo does not include a Dockerfile, Vedocker can:
- analyze the repository structure
- infer the stack
- generate a Dockerfile with AI
- validate the output
- continue into the normal build flow

That makes the system far more forgiving than a traditional container toolchain.

---

### 5. Learn infrastructure through a real working system
Vedocker is also a strong educational platform.

Because it exposes images, containers, builds, logs, orchestration state, scaling, and reconcile behavior through a working stack, it gives users a more concrete understanding of how modern infrastructure systems operate under the hood.

It is useful both as a product and as a systems learning surface.

---

## Major features

## 1. From-scratch container runtime

At the heart of Vedocker is a real container runtime built directly in Go and Linux.

This was not scaffolded on top of Docker. The core runtime behavior was implemented manually using the operating system primitives that containers rely on.

It supports:
- `create`
- `start`
- `stop`
- `rm`
- `logs`
- `exec`
- environment variables
- bind mounts
- runtime state management
- process isolation
- root filesystem execution
- lifecycle handling

Under the hood, this required real low-level systems work around:
- namespaces
- cgroups
- process control
- filesystem isolation
- runtime cleanup
- container state persistence

---

## 2. Full custom image system

Vedocker includes its own image store and metadata system.

Supported flows include:
- image add
- image list
- image inspect
- image remove
- image import
- image export
- image pull
- image build

This means the platform can manage reusable images like a real container engine instead of only running raw filesystem directories.

---

## 3. Dockerfile-style build engine

Vedocker includes a practical Dockerfile-lite builder that turns source code into runnable images.

Supported instructions include:
- `FROM`
- `COPY`
- `RUN`
- `WORKDIR`
- `ENV`
- `CMD`
- `ENTRYPOINT`
- `EXPOSE`

It also supports:
- multiline `RUN`
- COPY glob handling
- automatic base image pulling when needed

This was one of the biggest steps in making the platform feel real. Once image builds worked, Vedocker moved from “runtime project” to “actual developer platform.”

---

## 4. AI-assisted GitHub repo deployment

One of the strongest product features in Vedocker is the AI deployment pipeline.

A user can paste in a GitHub repository link, and Vedocker will:
1. clone the repository
2. inspect its contents
3. check whether a Dockerfile exists
4. build and run it if one is present
5. generate a Dockerfile with AI if one is missing
6. validate the generated file
7. continue through the build pipeline
8. launch the app inside the custom runtime
9. expose the result through the UI

This makes Vedocker especially useful for people who:
- do not know Docker well
- do not want to write Dockerfiles by hand
- want to quickly test or demo a repository
- want a smoother path from source code to running app

---

## 5. Daemon and live web UI

Vedocker includes a daemon and frontend dashboard that make the platform feel like a real product.

The UI supports:
- image management
- container management
- logs
- repo deployment
- Kubernetes-style resources
- cluster state visibility

This was an important part of the system because it turned the project from a command-line infrastructure engine into something users can actually interact with visually.

---

## 6. Kubernetes-style orchestration layer

After building the container engine, I built a Kubernetes-style control layer on top of it.

Supported features include:
- Pod apply
- Pod list
- Pod delete
- Deployment apply
- Deployment list
- Deployment delete
- smart replica scaling on re-apply
- reconcile one deployment
- reconcile all deployments
- background reconcile loop
- automatic self-healing
- Kubernetes dashboard views

This means Vedocker does not just run containers.

It manages desired state.

That shift made the project much closer to a true orchestration system rather than a single-container runtime.

---

## Why building it from scratch matters

What makes Vedocker especially compelling is not just what it does, but how it was built.

The platform was developed directly in the Linux terminal by working through the same categories of problems that real infrastructure software has to solve:
- how to isolate processes correctly
- how to constrain and manage execution
- how to build and register images
- how to wire networking and published ports
- how to track container state safely
- how to reconcile desired state against actual state
- how to recover automatically when workloads disappear or fail

This is the kind of project that forces a much deeper understanding of containerization and orchestration than simply using the tools themselves.

For a recruiter or engineer reading this, Vedocker demonstrates:
- low-level Linux systems knowledge
- backend and infrastructure design
- networking and debugging depth
- platform engineering thinking
- product thinking in developer tools
- the ability to build across the full stack, from kernel-adjacent runtime logic to user-facing interfaces

---

## Architecture

Vedocker is split into several layers.

### Layer 1: Runtime
Handles:
- process execution
- isolation
- rootfs setup
- namespaces
- cgroups
- lifecycle
- logs
- exec

### Layer 2: Networking
Handles:
- host-to-container communication
- internet egress
- published ports
- IP allocation
- iptables-based traffic forwarding

### Layer 3: Images
Handles:
- image storage
- metadata
- pull/import/export
- local image management
- rootfs registration

### Layer 4: Builder
Handles:
- Dockerfile-lite parsing
- filesystem changes
- build commands
- image creation from a build context

### Layer 5: Daemon and API
Handles:
- dashboard communication
- image and container routes
- repo deployment flow
- orchestration control endpoints
- Kubernetes data endpoints

### Layer 6: AI deploy pipeline
Handles:
- repository analysis
- Dockerfile generation fallback
- validation
- build continuation for repos missing containerization setup

### Layer 7: Orchestration
Handles:
- Pods
- Deployments
- replica state
- scaling
- reconcile logic
- self-healing
- background control loops

### Layer 8: Frontend
Handles:
- Docker-style dashboard
- Kubernetes-style dashboard
- repo deploy UI
- cluster state display
- logs and management controls

---

## System design flows

### Docker-style flow
1. User provides an image or GitHub repository
2. The daemon processes the request
3. The image is resolved or built
4. Runtime state is created
5. The container starts inside an isolated environment
6. Logs and lifecycle are exposed through the UI

### AI repo deploy flow
1. User pastes a GitHub repository URL
2. The daemon clones the repository
3. Vedocker checks for a Dockerfile
4. If missing, AI analyzes the repo and generates one
5. The builder creates an image
6. The runtime starts the container
7. The UI shows status, logs, and controls

### Kubernetes-style flow
1. User applies a Pod or Deployment
2. Desired state is written to local storage
3. Replicas are created on top of the custom engine
4. Reconcile logic compares desired state to actual state
5. Missing or failed replicas are recreated
6. A background reconcile loop provides self-healing
7. The Kubernetes dashboard displays current cluster state

---

## Biggest technical challenges

This project was difficult in the way real infrastructure projects are difficult.

Not because there was just a lot of code, but because the hard parts lived in debugging behavior across Linux, networking, process lifecycle, orchestration, and state management.

Some of the biggest challenges included:

### 1. Building a runtime from scratch
A container runtime sounds simple at a high level, but getting the behavior correct required careful handling of:
- state
- process IDs
- lifecycle transitions
- cleanup
- logs
- exec behavior
- rootfs execution

---

### 2. Linux isolation and security
The isolation layer required working through:
- namespaces
- cgroups
- how processes are launched
- how filesystems are prepared
- how execution inside a container differs from normal host execution

This was not ordinary app development. It was systems work.

---

### 3. Networking and port publishing
Networking was one of the most debugging-heavy parts of the platform.

The system had to support:
- host-to-container communication
- internet egress
- published host ports
- dynamic IP handling
- correct routing to running containers

This involved substantial Linux networking and iptables debugging.

---

### 4. Dockerfile build flow
Building images from Dockerfile instructions required translating build steps into reproducible filesystem and metadata state.

That included:
- instruction parsing
- working directory changes
- environment variable handling
- command execution
- file mutation
- image registration

---

### 5. AI fallback for repos without Dockerfiles
The AI deployment flow solved a real product problem, but it also introduced a design challenge.

It had to:
- inspect repo structure
- collect useful context
- generate a reasonable Dockerfile
- validate the result
- continue into the normal build pipeline safely

---

### 6. Building orchestration on top of a custom engine
Once Pods and Deployments existed, the project shifted from simple runtime execution to desired-state management.

That meant reasoning about:
- actual vs desired state
- idempotent apply behavior
- replica creation and replacement
- scaling correctness
- reconcile loops
- background repair

That is a very different systems mindset than simply starting a container.

---

## Engineering lessons

Vedocker taught me a huge amount about:
- Linux internals
- systems programming
- networking and iptables debugging
- state reconciliation
- infrastructure design
- platform engineering
- orchestration patterns
- developer-tool product design
- where complexity actually lives in container platforms

More than anything, it reinforced one major lesson:

**building a system from scratch is one of the fastest ways to understand the abstractions that most developers only ever use from the outside.**

---

## Current status

### Fully working
- from-scratch container runtime
- custom image system
- image pull, import, export, and build
- Dockerfile-lite build flow
- GitHub repo deployment
- AI-generated Dockerfile fallback
- daemon and Docker-style UI
- Pod management
- Deployment management
- smart replica scaling
- reconcile
- reconcile-all
- background self-healing
- Kubernetes dashboard
- service object and service state management

### Known limitation / future work
- fully robust multi-replica Service proxying and load balancing is future work because it requires a deeper redesign of the runtime networking model

This is an intentional boundary in the current version rather than hidden or incomplete behavior.

---

## Demo

### Screenshots
> Add screenshots here

- `[ Screenshot 1 ]`
- `[ Screenshot 2 ]`
- `[ Screenshot 3 ]`

### Demo video
> Add demo link here

- `[ Demo Video Link ]`

### Full build article / write-up
> Add your longer technical article here for readers who want the detailed engineering journey

- `[ Full Build Article Link ]`

---

## Example commands

### Runtime and image layer
```bash
./minicontainer image ls
./minicontainer image pull alpine:latest
./minicontainer image build -t demo:v1 -f Dockerfile .
./minicontainer create demo alpine:latest /bin/sh -c "sleep 3600"
./minicontainer start demo
./minicontainer logs demo
./minicontainer exec demo /bin/sh
