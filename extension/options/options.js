(function () {
  "use strict";

  const { DEFAULT_DASHBOARD_URL, normalizeDashboardURL, buildRunURL } = globalThis.VedockerRepo;
  const ext = globalThis.browser ?? globalThis.chrome;

  const form = document.getElementById("form");
  const input = document.getElementById("dashboard-url");
  const example = document.getElementById("example");
  const status = document.getElementById("status");
  const resetButton = document.getElementById("reset");

  function showExample(value) {
    const base = normalizeDashboardURL(value) || DEFAULT_DASHBOARD_URL;
    example.textContent = buildRunURL(base, { owner: "owner", repo: "repo" });
  }

  function setStatus(message, isError) {
    status.textContent = message;
    status.classList.toggle("error", Boolean(isError));
  }

  async function save(value) {
    const normalized = normalizeDashboardURL(value);
    if (!normalized) {
      setStatus("Enter an http:// or https:// URL, for example http://localhost:5173.", true);
      input.setAttribute("aria-invalid", "true");
      return;
    }
    input.removeAttribute("aria-invalid");
    await ext.storage.sync.set({ dashboardURL: normalized });
    input.value = normalized;
    showExample(normalized);
    setStatus("Saved. Open GitHub repo pages pick it up right away.");
  }

  form.addEventListener("submit", (event) => {
    event.preventDefault();
    save(input.value).catch((err) => setStatus(`Could not save: ${err.message}`, true));
  });

  resetButton.addEventListener("click", () => {
    input.value = DEFAULT_DASHBOARD_URL;
    save(DEFAULT_DASHBOARD_URL).catch((err) => setStatus(`Could not save: ${err.message}`, true));
  });

  input.addEventListener("input", () => {
    showExample(input.value);
    setStatus("");
  });

  ext.storage.sync
    .get({ dashboardURL: DEFAULT_DASHBOARD_URL })
    .then((items) => {
      input.value = items.dashboardURL;
      showExample(items.dashboardURL);
    })
    .catch((err) => setStatus(`Could not load settings: ${err.message}`, true));
})();
