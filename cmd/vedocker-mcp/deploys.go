package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	deployRunning   = "running"
	deploySucceeded = "succeeded"
	deployFailed    = "failed"
	deployNeedsAI   = "needs_ai"
)

// The daemon's POST /deployments/repo only returns once the clone, build and
// start are done, which can take minutes. deployTracker runs that request in
// the background so deploy_repo returns quickly and deploy_status can report
// progress. Records live in this process only.
type deployTracker struct {
	daemon  *daemonClient
	baseCtx context.Context

	mu     sync.Mutex
	nextID int
	jobs   map[string]*deployJob
}

type deployJob struct {
	id         string
	repoURL    string
	startedAt  time.Time
	finishedAt time.Time
	state      string
	result     *deployRepoResult
	err        string
	done       chan struct{}
}

func newDeployTracker(ctx context.Context, daemon *daemonClient) *deployTracker {
	return &deployTracker{daemon: daemon, baseCtx: ctx, jobs: map[string]*deployJob{}}
}

// start launches a deploy, or returns the in-flight one for the same repo so a
// retried tool call doesn't build the repo twice.
func (t *deployTracker) start(repoURL string) *deployJob {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, j := range t.jobs {
		if j.repoURL == repoURL && j.state == deployRunning {
			return j
		}
	}

	t.nextID++
	job := &deployJob{
		id:        "deploy-" + strconv.Itoa(t.nextID),
		repoURL:   repoURL,
		startedAt: time.Now(),
		state:     deployRunning,
		done:      make(chan struct{}),
	}
	t.jobs[job.id] = job

	go t.run(job)
	return job
}

func (t *deployTracker) run(job *deployJob) {
	ctx, cancel := context.WithTimeout(t.baseCtx, 30*time.Minute)
	defer cancel()

	res, err := t.daemon.deployRepo(ctx, job.repoURL)

	t.mu.Lock()
	defer t.mu.Unlock()
	defer close(job.done)

	job.finishedAt = time.Now()
	job.result = res
	switch {
	case res != nil && res.OK:
		job.state = deploySucceeded
	case res != nil && res.NeedsAI:
		job.state = deployNeedsAI
		job.err = res.Reason
	default:
		job.state = deployFailed
		if res != nil && res.Error != "" {
			job.err = res.Error
		} else if err != nil {
			job.err = err.Error()
		} else {
			job.err = "deploy failed without an error message"
		}
	}
}

func (t *deployTracker) get(id string) (*deployJob, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	j, ok := t.jobs[id]
	return j, ok
}

// all returns every deploy started by this server, newest first.
func (t *deployTracker) all() []*deployJob {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]*deployJob, 0, len(t.jobs))
	for _, j := range t.jobs {
		out = append(out, j)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].startedAt.After(out[b].startedAt) })
	return out
}

type deployStatus struct {
	DeployID       string         `json:"deploy_id"`
	RepoURL        string         `json:"repo_url"`
	State          string         `json:"state" jsonschema:"running, succeeded, failed or needs_ai"`
	StartedAt      string         `json:"started_at"`
	FinishedAt     string         `json:"finished_at,omitempty"`
	ElapsedSeconds int            `json:"elapsed_seconds"`
	ContainerID    string         `json:"container_id,omitempty"`
	ImageRef       string         `json:"image_ref,omitempty"`
	AIGenerated    bool           `json:"ai_generated,omitempty"`
	Container      *containerInfo `json:"container,omitempty"`
	Error          string         `json:"error,omitempty"`
	Message        string         `json:"message"`
}

// snapshot reports a job, enriched with the live container state once the
// deploy has produced a container.
func (t *deployTracker) snapshot(ctx context.Context, job *deployJob) deployStatus {
	t.mu.Lock()
	st := deployStatus{
		DeployID:  job.id,
		RepoURL:   job.repoURL,
		State:     job.state,
		StartedAt: job.startedAt.UTC().Format(time.RFC3339),
		Error:     job.err,
	}
	end := time.Now()
	if !job.finishedAt.IsZero() {
		end = job.finishedAt
		st.FinishedAt = job.finishedAt.UTC().Format(time.RFC3339)
	}
	st.ElapsedSeconds = int(end.Sub(job.startedAt).Seconds())
	if job.result != nil {
		st.ContainerID = job.result.ContainerID
		st.ImageRef = job.result.ImageRef
		st.AIGenerated = job.result.AIGenerated
	}
	t.mu.Unlock()

	if st.State == deploySucceeded && st.ContainerID != "" {
		if info, err := t.daemon.getContainer(ctx, st.ContainerID); err == nil {
			st.Container = info
		}
	}

	st.Message = deployMessage(st)
	return st
}

func deployMessage(st deployStatus) string {
	switch st.State {
	case deployRunning:
		return fmt.Sprintf("Still cloning and building %s (%ds so far). Call deploy_status with deploy_id %q again in about 15 seconds.", st.RepoURL, st.ElapsedSeconds, st.DeployID)
	case deploySucceeded:
		msg := fmt.Sprintf("Deployed %s as container %q from image %q.", st.RepoURL, st.ContainerID, st.ImageRef)
		if st.AIGenerated {
			msg += " The repo had no Dockerfile, so Gemini generated one."
		}
		if st.Container != nil {
			msg += fmt.Sprintf(" Container status: %s.", st.Container.Status)
			for _, p := range st.Container.Ports {
				msg += fmt.Sprintf(" Port %d is published on the daemon host at port %d.", p.ContainerPort, p.HostPort)
			}
		}
		return msg + " Use get_logs to see its output."
	case deployNeedsAI:
		return "The repo has no Dockerfile and the daemon has no Gemini API key. Restart the daemon with `sudo GEMINI_API_KEY=<key> ./minicontainerd` and deploy again."
	default:
		return "Deploy failed: " + st.Error
	}
}
