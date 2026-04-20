import React, { useEffect, useState } from "react";

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(await res.text());
  }
  return res.json();
}

async function postText(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "text/plain" },
    body,
  });
  if (!res.ok) {
    throw new Error(await res.text());
  }
}

async function postNoBody(url) {
  const res = await fetch(url, { method: "POST" });
  if (!res.ok) {
    throw new Error(await res.text());
  }
}

async function deleteReq(url) {
  const res = await fetch(url, { method: "DELETE" });
  if (!res.ok) {
    throw new Error(await res.text());
  }
}

export default function KubernetesPage() {
  const [pods, setPods] = useState([]);
  const [deployments, setDeployments] = useState([]);
  const [services, setServices] = useState([]);
  const [status, setStatus] = useState(null);

  const [deploymentYaml, setDeploymentYaml] = useState(`apiVersion: v1
kind: Deployment
metadata:
  name: demo
spec:
  replicas: 2
  template:
    image: alpine:latest
    command:
      - /bin/sh
      - -c
      - sleep 3600
    ports: []
`);

  const [serviceYaml, setServiceYaml] = useState(`apiVersion: v1
kind: Service
metadata:
  name: demo-svc
spec:
  deployment: demo
  port: 9000
  targetPort: 8080
`);

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");

  const refresh = async () => {
    const [podsData, deploymentsData, servicesData, statusData] = await Promise.all([
      getJSON("/api/k8s/pods"),
      getJSON("/api/k8s/deployments"),
      getJSON("/api/k8s/services"),
      getJSON("/api/k8s/status"),
    ]);

    setPods(Array.isArray(podsData) ? podsData : []);
    setDeployments(Array.isArray(deploymentsData) ? deploymentsData : []);
    setServices(Array.isArray(servicesData) ? servicesData : []);
    setStatus(statusData || null);
  };

  const runAction = async (fn, successMessage) => {
    setLoading(true);
    setError("");
    setMessage("");
    try {
      await fn();
      await refresh();
      setMessage(successMessage);
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refresh().catch((err) => {
      setError(err instanceof Error ? err.message : "failed to load kubernetes page");
    });

    const id = setInterval(() => {
      refresh().catch(() => {});
    }, 4000);

    return () => clearInterval(id);
  }, []);

  return (
    <div style={{ padding: 24 }}>
      <h1 style={{ marginBottom: 16 }}>Kubernetes</h1>

      {error ? (
        <div style={{
          marginBottom: 16,
          padding: 12,
          border: "1px solid #cc0000",
          borderRadius: 8,
          color: "#cc0000"
        }}>
          {error}
        </div>
      ) : null}

      {message ? (
        <div style={{
          marginBottom: 16,
          padding: 12,
          border: "1px solid #0a7f2e",
          borderRadius: 8,
          color: "#0a7f2e"
        }}>
          {message}
        </div>
      ) : null}

      <section style={{ marginBottom: 24, padding: 16, border: "1px solid #ddd", borderRadius: 10 }}>
        <h2 style={{ marginTop: 0 }}>Cluster / Auto-heal status</h2>
        <div style={{ marginBottom: 8 }}>
          Background reconcile: <strong>{status?.reconcile_loop_running ? "running" : "unknown"}</strong>
        </div>
        <div style={{ marginBottom: 12 }}>
          Services: <strong>{status?.service_layer_status || "unknown"}</strong>
        </div>
        <button
          disabled={loading}
          onClick={() => runAction(() => postNoBody("/api/k8s/reconcile-all"), "Reconcile all completed")}
        >
          Reconcile all
        </button>
      </section>

      <section style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, marginBottom: 24 }}>
        <div style={{ padding: 16, border: "1px solid #ddd", borderRadius: 10 }}>
          <h2 style={{ marginTop: 0 }}>Apply deployment</h2>
          <textarea
            value={deploymentYaml}
            onChange={(e) => setDeploymentYaml(e.target.value)}
            rows={16}
            style={{ width: "100%", fontFamily: "monospace", marginBottom: 12 }}
          />
          <button
            disabled={loading}
            onClick={() => runAction(() => postText("/api/k8s/deployments", deploymentYaml), "Deployment applied")}
          >
            Apply deployment
          </button>
        </div>

        <div style={{ padding: 16, border: "1px solid #ddd", borderRadius: 10 }}>
          <h2 style={{ marginTop: 0 }}>Apply service</h2>
          <textarea
            value={serviceYaml}
            onChange={(e) => setServiceYaml(e.target.value)}
            rows={16}
            style={{ width: "100%", fontFamily: "monospace", marginBottom: 12 }}
          />
          <button
            disabled={loading}
            onClick={() => runAction(() => postText("/api/k8s/services", serviceYaml), "Service applied")}
          >
            Apply service
          </button>
        </div>
      </section>

      <section style={{ marginBottom: 24 }}>
        <h2>Pods</h2>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th style={th}>Name</th>
              <th style={th}>Container</th>
              <th style={th}>IP</th>
              <th style={th}>Status</th>
              <th style={th}>Updated</th>
            </tr>
          </thead>
          <tbody>
            {pods.map((p) => (
              <tr key={p.name}>
                <td style={td}>{p.name}</td>
                <td style={td}>{p.container_name}</td>
                <td style={td}>{p.ip || "-"}</td>
                <td style={td}>{p.status || "-"}</td>
                <td style={td}>{p.updated_at}</td>
              </tr>
            ))}
            {pods.length === 0 ? (
              <tr>
                <td style={td} colSpan={5}>No pods</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </section>

      <section style={{ marginBottom: 24 }}>
        <h2>Deployments</h2>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th style={th}>Name</th>
              <th style={th}>Replicas</th>
              <th style={th}>Updated</th>
              <th style={th}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {deployments.map((d) => (
              <tr key={d.name}>
                <td style={td}>{d.name}</td>
                <td style={td}>{d.replicas}</td>
                <td style={td}>{d.updated_at}</td>
                <td style={td}>
                  <button
                    disabled={loading}
                    onClick={() =>
                      runAction(
                        () => deleteReq(`/api/k8s/deployments/${encodeURIComponent(d.name)}`),
                        `Deployment ${d.name} deleted`
                      )
                    }
                  >
                    Delete deployment
                  </button>
                </td>
              </tr>
            ))}
            {deployments.length === 0 ? (
              <tr>
                <td style={td} colSpan={4}>No deployments</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </section>

      <section style={{ marginBottom: 24 }}>
        <h2>Services</h2>
        <div style={{ marginBottom: 10, color: "#555" }}>
          Working now: Service state/config management. Future work: full reliable multi-replica load balancing.
        </div>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <th style={th}>Name</th>
              <th style={th}>Deployment</th>
              <th style={th}>Frontend Port</th>
              <th style={th}>Target Port</th>
              <th style={th}>Updated</th>
              <th style={th}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {services.map((s) => (
              <tr key={s.name}>
                <td style={td}>{s.name}</td>
                <td style={td}>{s.deployment}</td>
                <td style={td}>{s.port}</td>
                <td style={td}>{s.target_port}</td>
                <td style={td}>{s.updated_at}</td>
                <td style={td}>
                  <button
                    disabled={loading}
                    onClick={() =>
                      runAction(
                        () => deleteReq(`/api/k8s/services/${encodeURIComponent(s.name)}`),
                        `Service ${s.name} deleted`
                      )
                    }
                  >
                    Delete service
                  </button>
                </td>
              </tr>
            ))}
            {services.length === 0 ? (
              <tr>
                <td style={td} colSpan={6}>No services</td>
              </tr>
            ) : null}
          </tbody>
        </table>
      </section>
    </div>
  );
}

const th = {
  textAlign: "left",
  borderBottom: "1px solid #ddd",
  padding: "10px 8px",
};

const td = {
  borderBottom: "1px solid #eee",
  padding: "10px 8px",
};