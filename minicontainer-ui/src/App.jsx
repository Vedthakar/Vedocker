import React, { useEffect, useMemo, useState } from "react";

const API_BASE = "/api";

async function apiFetch(path, options = {}) {
  const response = await fetch(`${API_BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
    ...options,
  });

  let data = null;
  const text = await response.text();
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }

  if (!response.ok) {
    const message =
      (data && typeof data === "object" && data.error) ||
      `Request failed: ${response.status}`;
    throw new Error(message);
  }

  return data;
}

function formatDate(value) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function classForStatus(status) {
  const normalized = String(status || "").toLowerCase();
  if (normalized.includes("running")) return "status-pill running";
  if (normalized.includes("stopped") || normalized.includes("exited")) {
    return "status-pill stopped";
  }
  return "status-pill neutral";
}

function extractHostPorts(container) {
  const ports = container?.ports;
  if (!Array.isArray(ports)) return [];

  return ports
    .map((p) => {
      const hostPort =
        p?.host_port ??
        p?.hostPort ??
        p?.host ??
        p?.published ??
        p?.HostPort;

      const numeric = Number(hostPort);
      if (!Number.isFinite(numeric) || numeric <= 0) return null;
      return numeric;
    })
    .filter(Boolean);
}

function accessNote(container) {
  const status = String(container?.status || "").toLowerCase();
  if (!status.includes("running")) return null;

  const hostPorts = extractHostPorts(container);
  if (hostPorts.length === 0) {
    return {
      kind: "none",
      text: "Running, but no published port",
    };
  }

  return {
    kind: "links",
    links: [...new Set(hostPorts)].map((port) => ({
      port,
      url: `http://localhost:${port}`,
    })),
  };
}

function App() {
  const [geminiAPIKey, setGeminiAPIKey] = useState("");
  const [images, setImages] = useState([]);
  const [containers, setContainers] = useState([]);
  const [daemonOnline, setDaemonOnline] = useState(false);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [selectedContainerId, setSelectedContainerId] = useState("");
  const [logStream, setLogStream] = useState("stdout");
  const [logs, setLogs] = useState("");
  const [logsLoading, setLogsLoading] = useState(false);
  const [error, setError] = useState("");
  const [actionBusyId, setActionBusyId] = useState("");
  const [imageBusyRef, setImageBusyRef] = useState("");
  const [createBusy, setCreateBusy] = useState(false);
  const [showCreateContainer, setShowCreateContainer] = useState(false);

  const [repoURL, setRepoURL] = useState("");
  const [deployBusy, setDeployBusy] = useState(false);
  const [deployResult, setDeployResult] = useState(null);

  const [newContainerId, setNewContainerId] = useState("");
  const [newContainerImage, setNewContainerImage] = useState("");
  const [newContainerCommand, setNewContainerCommand] = useState("sleep 300");

  const selectedContainer = useMemo(
    () => containers.find((c) => c.id === selectedContainerId) || null,
    [containers, selectedContainerId]
  );

  async function loadDashboard(showSpinner = true) {
    if (showSpinner) setLoading(true);
    else setRefreshing(true);

    setError("");

    try {
      const [imagesRes, containersRes] = await Promise.all([
        apiFetch("/images"),
        apiFetch("/containers"),
      ]);

      const nextImages = Array.isArray(imagesRes?.images) ? imagesRes.images : [];
      const nextContainers = Array.isArray(containersRes?.containers)
        ? containersRes.containers
        : [];

      setImages(nextImages);
      setContainers(nextContainers);
      setDaemonOnline(true);

      if (!newContainerImage && nextImages.length > 0) {
        setNewContainerImage(nextImages[0].ref || "");
      } else if (
        newContainerImage &&
        !nextImages.some((img) => img.ref === newContainerImage)
      ) {
        setNewContainerImage(nextImages[0]?.ref || "");
      }

      if (selectedContainerId) {
        const stillExists = nextContainers.some((c) => c.id === selectedContainerId);
        if (!stillExists) {
          setSelectedContainerId("");
          setLogs("");
        }
      }
    } catch (err) {
      setDaemonOnline(false);
      setError(err.message || "Could not reach daemon");
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }

  async function loadLogs(containerId, stream) {
    if (!containerId) {
      setLogs("");
      return;
    }

    setLogsLoading(true);
    try {
      const data = await apiFetch(
        `/containers/${encodeURIComponent(containerId)}/logs?stream=${encodeURIComponent(stream)}`
      );
      setLogs(data?.logs || "");
    } catch (err) {
      setLogs(`error: ${err.message || "failed to load logs"}`);
    } finally {
      setLogsLoading(false);
    }
  }

  async function handleContainerAction(id, action) {
    const actionMap = {
      start: { method: "POST", path: `/containers/${encodeURIComponent(id)}/start` },
      stop: { method: "POST", path: `/containers/${encodeURIComponent(id)}/stop` },
      remove: { method: "DELETE", path: `/containers/${encodeURIComponent(id)}` },
    };

    const config = actionMap[action];
    if (!config) return;

    setActionBusyId(id);
    setError("");

    try {
      await apiFetch(config.path, { method: config.method });
      await loadDashboard(false);

      if (selectedContainerId === id && action === "remove") {
        setSelectedContainerId("");
        setLogs("");
      } else if (selectedContainerId === id) {
        await loadLogs(id, logStream);
      }
    } catch (err) {
      setError(err.message || `Failed to ${action} container`);
    } finally {
      setActionBusyId("");
    }
  }

  async function handleCreateContainer(event) {
    event.preventDefault();

    const id = newContainerId.trim();
    const image = newContainerImage.trim();
    const command = newContainerCommand.trim();

    if (!id) {
      setError("Container id is required");
      return;
    }
    if (!image) {
      setError("Select an image");
      return;
    }
    if (!command) {
      setError("Command is required");
      return;
    }

    setCreateBusy(true);
    setError("");

    try {
      await apiFetch("/containers/create", {
        method: "POST",
        body: JSON.stringify({
          id,
          image,
          command: ["/bin/sh", "-c", command],
        }),
      });

      setNewContainerId("");
      setShowCreateContainer(false);
      await loadDashboard(false);
    } catch (err) {
      setError(err.message || "Failed to create container");
    } finally {
      setCreateBusy(false);
    }
  }

  async function handleDeleteImage(ref) {
    setImageBusyRef(ref);
    setError("");

    try {
      await apiFetch(`/images/${encodeURIComponent(ref)}`, {
        method: "DELETE",
      });

      await loadDashboard(false);
    } catch (err) {
      setError(err.message || "Failed to delete image");
    } finally {
      setImageBusyRef("");
    }
  }

  async function handleDeployRepo(event) {
    event.preventDefault();

    const trimmed = repoURL.trim();
    if (!trimmed) {
      setError("Repo URL is required");
      return;
    }

    setDeployBusy(true);
    setError("");
    setDeployResult(null);

    try {
      const result = await apiFetch("/deployments/repo", {
        method: "POST",
        body: JSON.stringify({
            repo_url: trimmed,
            gemini_api_key: geminiAPIKey.trim(),
        }),
      });

      setDeployResult(result);
      await loadDashboard(false);

      if (result?.container_id) {
        setSelectedContainerId(result.container_id);
        await loadLogs(result.container_id, "stdout");
        setLogStream("stdout");
      }
    } catch (err) {
      setDeployResult(null);
      setError(err.message || "Failed to deploy repo");
    } finally {
      setDeployBusy(false);
    }
  }

  useEffect(() => {
    loadDashboard(true);
  }, []);

  useEffect(() => {
    if (selectedContainerId) {
      loadLogs(selectedContainerId, logStream);
    }
  }, [selectedContainerId, logStream]);

  return (
    <div className="shell">
      <div className="background-glow background-glow-a" />
      <div className="background-glow background-glow-b" />

      <header className="hero card">
        <div>
          <div className="eyebrow">Local Engine Dashboard</div>
          <h1>Minicontainer Control Surface</h1>
          <p className="hero-copy">
            A minimal command center for your custom container engine.
          </p>
        </div>

        <div className="hero-side">
          <div className={`daemon-indicator ${daemonOnline ? "online" : "offline"}`}>
            <span className="dot" />
            <span>{daemonOnline ? "Daemon reachable" : "Daemon offline"}</span>
          </div>

          <button
            className="primary-btn"
            onClick={() => loadDashboard(false)}
            disabled={refreshing || loading}
          >
            {refreshing ? "Refreshing..." : "Refresh"}
          </button>
        </div>
      </header>

      <section className="deploy-panel card">
        <div className="panel-header deploy-header">
          <div>
            <div className="panel-kicker">Repo Deploy</div>
            <h2>Deploy GitHub Repo</h2>
          </div>
        </div>

       <form className="deploy-form" onSubmit={handleDeployRepo}>
        <input
            type="text"
            value={repoURL}
            onChange={(e) => setRepoURL(e.target.value)}
            placeholder="https://github.com/owner/repo"
        />

        <input
            type="password"
            value={geminiAPIKey}
            onChange={(e) => setGeminiAPIKey(e.target.value)}
            placeholder="Gemini API key (used only if no Dockerfile is found)"
        />

        <button className="primary-btn" type="submit" disabled={deployBusy}>
            {deployBusy ? "Deploying..." : "Deploy"}
        </button>
        </form>
        <div className="muted" style={{ marginTop: "10px" }}>
        Only used if no Dockerfile is found. Paste your Gemini API key here.
        </div>
        {deployResult && (
          <div
            className={`deploy-result ${
              deployResult.ok
                ? "success"
                : deployResult.needs_ai
                ? "needs-ai"
                : "failure"
            }`}
          >
            <div className="deploy-result-title">
              {deployResult.ok
                ? "Deployment succeeded"
                : deployResult.needs_ai
                ? "AI fallback needed"
                : "Deployment failed"}
            </div>

            <div className="deploy-result-grid">
              <div>
                <span className="info-label">Repo</span>
                <span className="mono dim wrap-anywhere">
                  {deployResult.repo_url || "—"}
                </span>
              </div>

              {deployResult.image_ref && (
                <div>
                  <span className="info-label">Image</span>
                  <span className="mono dim">{deployResult.image_ref}</span>
                </div>
              )}

              {deployResult.container_id && (
                <div>
                  <span className="info-label">Container</span>
                  <button
                    type="button"
                    className="inline-link mono"
                    onClick={() => setSelectedContainerId(deployResult.container_id)}
                  >
                    {deployResult.container_id}
                  </button>
                </div>
              )}

              {deployResult.dockerfile_path && (
                <div>
                  <span className="info-label">Dockerfile</span>
                  <span className="mono dim wrap-anywhere">
                    {deployResult.dockerfile_path}
                  </span>
                </div>
              )}

              {deployResult.reason && (
                <div>
                  <span className="info-label">Reason</span>
                  <span>{deployResult.reason}</span>
                </div>
              )}

              {deployResult.error && (
                <div>
                  <span className="info-label">Error</span>
                  <span>{deployResult.error}</span>
                </div>
              )}
              {deployResult.ai_generated && (
                <div>
                    <span className="info-label">AI</span>
                    <span>Dockerfile was generated by AI fallback</span>
                </div>
                )}
            </div>
          </div>
        )}
      </section>

      {error && (
        <section className="alert card">
          <span className="alert-label">Notice</span>
          <span>{error}</span>
        </section>
      )}

      <section className="stats-grid">
        <div className="stat-card card">
          <div className="stat-label">Images</div>
          <div className="stat-value">{images.length}</div>
        </div>

        <div className="stat-card card">
          <div className="stat-label">Containers</div>
          <div className="stat-value">{containers.length}</div>
        </div>

        <div className="stat-card card">
          <div className="stat-label">Selected</div>
          <div className="stat-value small">
            {selectedContainerId || "None"}
          </div>
        </div>
      </section>

      <main className="main-grid">
        <section className="panel card">
          <div className="panel-header">
            <div>
              <div className="panel-kicker">Registry</div>
              <h2>Images</h2>
            </div>
          </div>

          {loading ? (
            <div className="empty-state">Loading images...</div>
          ) : images.length === 0 ? (
            <div className="empty-state">No images found.</div>
          ) : (
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Ref</th>
                    <th>Source</th>
                    <th>Rootfs</th>
                    <th>Created</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  {images.map((image) => (
                    <tr key={image.ref}>
                      <td className="mono strong">{image.ref || "—"}</td>
                      <td>{image.source || "—"}</td>
                      <td className="mono dim">{image.rootfs || "—"}</td>
                      <td>{formatDate(image.created_at)}</td>
                      <td>
                        <button
                          className="action-btn remove"
                          disabled={imageBusyRef === image.ref}
                          onClick={() => handleDeleteImage(image.ref)}
                        >
                          {imageBusyRef === image.ref ? "Deleting..." : "Delete"}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <div className="collapsible-wrap">
            <button
              type="button"
              className="secondary-btn"
              onClick={() => setShowCreateContainer((prev) => !prev)}
            >
              {showCreateContainer ? "Hide Create Container" : "Create Container"}
            </button>

            {showCreateContainer && (
              <div className="create-panel">
                <div className="panel-kicker">Create</div>
                <h3>Create Container</h3>

                <form className="create-form" onSubmit={handleCreateContainer}>
                  <div className="field-group">
                    <label htmlFor="container-id">Container ID</label>
                    <input
                      id="container-id"
                      type="text"
                      value={newContainerId}
                      onChange={(e) => setNewContainerId(e.target.value)}
                      placeholder="livedemo2"
                    />
                  </div>

                  <div className="field-group">
                    <label htmlFor="container-image">Image</label>
                    <select
                      id="container-image"
                      value={newContainerImage}
                      onChange={(e) => setNewContainerImage(e.target.value)}
                    >
                      <option value="">Select an image</option>
                      {images.map((image) => (
                        <option key={image.ref} value={image.ref}>
                          {image.ref}
                        </option>
                      ))}
                    </select>
                  </div>

                  <div className="field-group">
                    <label htmlFor="container-command">Command</label>
                    <input
                      id="container-command"
                      type="text"
                      value={newContainerCommand}
                      onChange={(e) => setNewContainerCommand(e.target.value)}
                      placeholder="sleep 300"
                    />
                  </div>

                  <button
                    className="primary-btn"
                    type="submit"
                    disabled={createBusy || images.length === 0}
                  >
                    {createBusy ? "Creating..." : "Create Container"}
                  </button>
                </form>
              </div>
            )}
          </div>
        </section>

        <section className="panel card">
          <div className="panel-header">
            <div>
              <div className="panel-kicker">Runtime</div>
              <h2>Containers</h2>
            </div>
          </div>

          {loading ? (
            <div className="empty-state">Loading containers...</div>
          ) : containers.length === 0 ? (
            <div className="empty-state">No containers found.</div>
          ) : (
            <div className="container-list">
              {containers.map((container) => {
                const isSelected = selectedContainerId === container.id;
                const busy = actionBusyId === container.id;
                const access = accessNote(container);

                return (
                  <div
                    key={container.id}
                    className={`container-card ${isSelected ? "selected" : ""}`}
                    onClick={() => setSelectedContainerId(container.id)}
                  >
                    <div className="container-top">
                      <div>
                        <div className="container-id mono">{container.id || "—"}</div>
                        <div className={classForStatus(container.status)}>
                          {container.status || "unknown"}
                        </div>
                      </div>

                      <div className="container-meta">
                        <span className="meta-chip">PID {container.pid ?? "—"}</span>
                      </div>
                    </div>

                    <div className="container-info">
                      <div>
                        <span className="info-label">Rootfs</span>
                        <span className="mono dim truncate">{container.rootfs || "—"}</span>
                      </div>

                      <div>
                        <span className="info-label">Created</span>
                        <span>{formatDate(container.created_at)}</span>
                      </div>

                      {access && (
                        <div>
                          <span className="info-label">Access</span>

                          {access.kind === "links" ? (
                            <div className="access-links">
                              {access.links.map((link) => (
                                <a
                                  key={link.port}
                                  href={link.url}
                                  target="_blank"
                                  rel="noreferrer"
                                  className="inline-link mono"
                                  onClick={(e) => e.stopPropagation()}
                                >
                                  {link.url}
                                </a>
                              ))}
                            </div>
                          ) : (
                            <span className="muted">{access.text}</span>
                          )}
                        </div>
                      )}
                    </div>

                    <div
                      className="action-row"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <button
                        className="action-btn start"
                        disabled={busy}
                        onClick={() => handleContainerAction(container.id, "start")}
                      >
                        Start
                      </button>
                      <button
                        className="action-btn stop"
                        disabled={busy}
                        onClick={() => handleContainerAction(container.id, "stop")}
                      >
                        Stop
                      </button>
                      <button
                        className="action-btn remove"
                        disabled={busy}
                        onClick={() => handleContainerAction(container.id, "remove")}
                      >
                        Remove
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </section>
      </main>

      <section className="logs-panel card">
        <div className="panel-header logs-header">
          <div>
            <div className="panel-kicker">Inspector</div>
            <h2>Logs Viewer</h2>
          </div>

          <div className="logs-controls">
            <div className="log-target">
              {selectedContainer ? (
                <>
                  <span className="muted">Container</span>
                  <span className="mono strong">{selectedContainer.id}</span>
                </>
              ) : (
                <span className="muted">Select a container</span>
              )}
            </div>

            <div className="segmented">
              <button
                className={logStream === "stdout" ? "active" : ""}
                onClick={() => setLogStream("stdout")}
              >
                stdout
              </button>
              <button
                className={logStream === "stderr" ? "active" : ""}
                onClick={() => setLogStream("stderr")}
              >
                stderr
              </button>
            </div>
          </div>
        </div>

        <div className="console">
          {!selectedContainerId ? (
            <div className="console-empty">Select a container to inspect logs.</div>
          ) : logsLoading ? (
            <div className="console-empty">Loading logs...</div>
          ) : logs ? (
            <pre>{logs}</pre>
          ) : (
            <div className="console-empty">No log output for this stream.</div>
          )}
        </div>
      </section>
    </div>
  );
}

export default App;