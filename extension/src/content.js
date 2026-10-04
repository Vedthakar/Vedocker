// Adds a "Run with Vedocker" button next to the green Code button on GitHub
// repo pages. GitHub swaps page content without full reloads (Turbo and React
// soft navigation), so the button is re-checked whenever the DOM changes.
(function () {
  "use strict";

  const { DEFAULT_DASHBOARD_URL, parseRepoFromPath, normalizeDashboardURL, buildRunURL } = globalThis.VedockerRepo;
  const ext = globalThis.browser ?? globalThis.chrome;
  const BUTTON_ID = "vedocker-run-button";
  const SVG_NS = "http://www.w3.org/2000/svg";

  let dashboardURL = DEFAULT_DASHBOARD_URL;
  let scheduled = false;

  function findCodeButton() {
    // Current React UI first, then the older <details> based markup.
    const candidates = document.querySelectorAll('button[data-variant="primary"], summary.btn-primary');
    for (const el of candidates) {
      if (el.textContent.trim() === "Code") return el;
    }
    return null;
  }

  function createIcon() {
    const svg = document.createElementNS(SVG_NS, "svg");
    svg.setAttribute("viewBox", "0 0 16 16");
    svg.setAttribute("width", "16");
    svg.setAttribute("height", "16");
    svg.setAttribute("aria-hidden", "true");
    const path = document.createElementNS(SVG_NS, "path");
    path.setAttribute("fill", "currentColor");
    path.setAttribute(
      "d",
      "M1.5 3.25C1.5 2.284 2.284 1.5 3.25 1.5h9.5c.966 0 1.75.784 1.75 1.75v9.5a1.75 1.75 0 0 1-1.75 1.75h-9.5A1.75 1.75 0 0 1 1.5 12.75Zm1.75-.25a.25.25 0 0 0-.25.25v9.5c0 .138.112.25.25.25h9.5a.25.25 0 0 0 .25-.25v-9.5a.25.25 0 0 0-.25-.25ZM6 5.54c0-.39.42-.63.75-.43l3.94 2.46c.31.2.31.66 0 .86L6.75 10.9A.5.5 0 0 1 6 10.46Z"
    );
    svg.appendChild(path);
    return svg;
  }

  function createButton(href, repo) {
    const a = document.createElement("a");
    a.id = BUTTON_ID;
    a.className = "vedocker-run-button";
    a.href = href;
    a.target = "_blank";
    a.rel = "noopener noreferrer";
    a.setAttribute("aria-label", "Run with Vedocker");
    a.title = `Build and run ${repo.owner}/${repo.repo} in your Vedocker dashboard`;
    a.appendChild(createIcon());
    const label = document.createElement("span");
    label.textContent = "Run with Vedocker";
    a.appendChild(label);
    return a;
  }

  function render() {
    scheduled = false;

    const existing = document.getElementById(BUTTON_ID);
    const repo = parseRepoFromPath(location.pathname);
    const codeButton = repo ? findCodeButton() : null;

    if (!codeButton) {
      if (existing) existing.remove();
      return;
    }

    const href = buildRunURL(dashboardURL, repo);
    if (existing && existing.previousElementSibling === codeButton) {
      if (existing.getAttribute("href") !== href) {
        existing.href = href;
        existing.title = `Build and run ${repo.owner}/${repo.repo} in your Vedocker dashboard`;
      }
      return;
    }

    if (existing) existing.remove();
    codeButton.after(createButton(href, repo));
  }

  function schedule() {
    if (scheduled) return;
    scheduled = true;
    requestAnimationFrame(render);
  }

  function setDashboardURL(value) {
    dashboardURL = normalizeDashboardURL(value) || DEFAULT_DASHBOARD_URL;
    schedule();
  }

  if (ext?.storage?.sync) {
    ext.storage.sync
      .get({ dashboardURL: DEFAULT_DASHBOARD_URL })
      .then((items) => setDashboardURL(items.dashboardURL))
      .catch(() => {});

    ext.storage.onChanged.addListener((changes, area) => {
      if (area === "sync" && changes.dashboardURL) {
        setDashboardURL(changes.dashboardURL.newValue);
      }
    });
  }

  // Turbo, legacy pjax and React soft-navigation events, plus a DOM observer
  // for anything they miss (lazy-rendered headers, back/forward cache).
  for (const event of ["turbo:load", "turbo:render", "pjax:end", "soft-nav:end", "popstate", "pageshow"]) {
    window.addEventListener(event, schedule);
    document.addEventListener(event, schedule);
  }
  new MutationObserver(schedule).observe(document.documentElement, { childList: true, subtree: true });

  schedule();
})();
