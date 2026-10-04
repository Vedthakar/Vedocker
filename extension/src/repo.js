// Pure helpers shared by the content script and the options page.
// Loaded as a classic script in the extension and as a CommonJS module in tests.
(function (root) {
  "use strict";

  const DEFAULT_DASHBOARD_URL = "http://localhost:5173";

  // First path segments on github.com that are site pages, not repo owners.
  const RESERVED_OWNERS = new Set([
    "about", "account", "apps", "codespaces", "collections", "contact", "copilot",
    "customer-stories", "dashboard", "enterprise", "events", "explore", "features",
    "github-copilot", "issues", "join", "login", "logout", "marketplace", "models",
    "new", "notifications", "organizations", "orgs", "pricing", "pulls", "readme",
    "search", "security", "sessions", "settings", "signup", "site", "solutions",
    "sponsors", "stars", "team", "topics", "trending", "users",
  ]);

  const OWNER_PATTERN = /^[A-Za-z0-9](?:[A-Za-z0-9]|-[A-Za-z0-9]){0,38}$/;
  const REPO_PATTERN = /^[A-Za-z0-9._-]{1,100}$/;

  // parseRepoFromPath("/owner/repo/tree/main") -> { owner: "owner", repo: "repo" }
  function parseRepoFromPath(pathname) {
    const parts = String(pathname || "").split("/").filter(Boolean);
    if (parts.length < 2) return null;

    const owner = parts[0];
    const repo = parts[1].replace(/\.git$/, "");
    if (RESERVED_OWNERS.has(owner.toLowerCase())) return null;
    if (!OWNER_PATTERN.test(owner)) return null;
    if (!REPO_PATTERN.test(repo) || repo === "." || repo === "..") return null;
    return { owner, repo };
  }

  // Returns the dashboard origin (plus any base path) without a trailing
  // slash, or null if the value is not a plain http(s) URL.
  function normalizeDashboardURL(value) {
    let s = String(value || "").trim();
    if (!s) return null;
    if (!/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) s = "http://" + s;

    let url;
    try {
      url = new URL(s);
    } catch {
      return null;
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    if (url.username || url.password) return null;
    if (!url.hostname) return null;

    const path = url.pathname.replace(/\/+$/, "");
    return url.origin + path;
  }

  // buildRunURL("http://localhost:5173", {owner, repo})
  //   -> "http://localhost:5173/github.com/owner/repo"
  function buildRunURL(dashboardURL, repo) {
    const base = normalizeDashboardURL(dashboardURL) || DEFAULT_DASHBOARD_URL;
    return `${base}/github.com/${encodeURIComponent(repo.owner)}/${encodeURIComponent(repo.repo)}`;
  }

  const api = { DEFAULT_DASHBOARD_URL, parseRepoFromPath, normalizeDashboardURL, buildRunURL };

  if (typeof module !== "undefined" && module.exports) {
    module.exports = api;
  } else {
    root.VedockerRepo = api;
  }
})(typeof globalThis !== "undefined" ? globalThis : this);
