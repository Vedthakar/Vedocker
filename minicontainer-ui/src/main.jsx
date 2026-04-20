import React, { useState } from "react";
import ReactDOM from "react-dom/client";
import App from "./App";
import KubernetesPage from "./KubernetesPage";
import "./styles.css";

function Root() {
  const [view, setView] = useState("docker");

  return (
    <React.StrictMode>
      <div style={{ padding: 16 }}>
        <div style={{ display: "flex", gap: 12, marginBottom: 16 }}>
          <button
            onClick={() => setView("docker")}
            style={{
              padding: "10px 14px",
              borderRadius: 10,
              border: "1px solid #ccc",
              background: view === "docker" ? "#eee" : "white",
            }}
          >
            Docker
          </button>
          <button
            onClick={() => setView("kubernetes")}
            style={{
              padding: "10px 14px",
              borderRadius: 10,
              border: "1px solid #ccc",
              background: view === "kubernetes" ? "#eee" : "white",
            }}
          >
            Kubernetes
          </button>
        </div>

        {view === "docker" ? <App /> : <KubernetesPage />}
      </div>
    </React.StrictMode>
  );
}

ReactDOM.createRoot(document.getElementById("root")).render(<Root />);