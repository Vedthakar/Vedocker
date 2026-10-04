const test = require("node:test");
const assert = require("node:assert/strict");
const { DEFAULT_DASHBOARD_URL, parseRepoFromPath, normalizeDashboardURL, buildRunURL } = require("../src/repo.js");

test("parseRepoFromPath finds owner and repo on repo pages", () => {
  assert.deepEqual(parseRepoFromPath("/owner/repo"), { owner: "owner", repo: "repo" });
  assert.deepEqual(parseRepoFromPath("/owner/repo/"), { owner: "owner", repo: "repo" });
  assert.deepEqual(parseRepoFromPath("/owner/repo/tree/main/src"), { owner: "owner", repo: "repo" });
  assert.deepEqual(parseRepoFromPath("/Owner-1/My.Repo_2"), { owner: "Owner-1", repo: "My.Repo_2" });
  assert.deepEqual(parseRepoFromPath("/owner/repo.git"), { owner: "owner", repo: "repo" });
});

test("parseRepoFromPath ignores non-repo pages", () => {
  for (const path of ["/", "", "/owner", "/settings/profile", "/orgs/acme", "/marketplace/actions",
    "/topics/go", "/features/copilot", "/-bad/repo", "/owner/..", "/sponsors/someone", "/notifications/beta"]) {
    assert.equal(parseRepoFromPath(path), null, path);
  }
});

test("normalizeDashboardURL accepts http(s) URLs and strips trailing slashes", () => {
  assert.equal(normalizeDashboardURL("http://localhost:5173"), "http://localhost:5173");
  assert.equal(normalizeDashboardURL("http://localhost:5173/"), "http://localhost:5173");
  assert.equal(normalizeDashboardURL("localhost:5173"), "http://localhost:5173");
  assert.equal(normalizeDashboardURL("  https://vedocker.example.com/  "), "https://vedocker.example.com");
  assert.equal(normalizeDashboardURL("http://box.local:5173/vedocker/"), "http://box.local:5173/vedocker");
  assert.equal(normalizeDashboardURL("http://localhost:5173/?x=1#y"), "http://localhost:5173");
});

test("normalizeDashboardURL rejects unsafe or invalid values", () => {
  for (const value of ["", "   ", "javascript:alert(1)", "javascript://%0aalert(1)", "data:text/html,hi",
    "file:///etc/passwd", "ftp://localhost", "http://user:pass@localhost:5173", "http://"]) {
    assert.equal(normalizeDashboardURL(value), null, value);
  }
});

test("buildRunURL swaps github.com for the dashboard", () => {
  assert.equal(buildRunURL("http://localhost:5173", { owner: "owner", repo: "repo" }), "http://localhost:5173/github.com/owner/repo");
  assert.equal(buildRunURL("http://localhost:5173/", { owner: "a", repo: "b.c" }), "http://localhost:5173/github.com/a/b.c");
  assert.equal(buildRunURL("javascript:alert(1)", { owner: "a", repo: "b" }), `${DEFAULT_DASHBOARD_URL}/github.com/a/b`);
});

test("manifest only asks for storage and github.com", () => {
  const manifest = require("../manifest.json");
  assert.equal(manifest.manifest_version, 3);
  assert.deepEqual(manifest.permissions, ["storage"]);
  assert.equal(manifest.host_permissions, undefined);
  assert.equal(manifest.background, undefined);
  assert.deepEqual(manifest.content_scripts.map((c) => c.matches).flat(), ["https://github.com/*"]);
});
