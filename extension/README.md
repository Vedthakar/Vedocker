# Run with Vedocker (browser extension)

Adds a **Run with Vedocker** button next to the green **Code** button on GitHub repo pages. Click it and the repo opens in your Vedocker dashboard, which clones, builds and runs it. It's the [URL trick](../README.md#the-url-trick) as a button.

- Manifest V3. Works in Chrome, Edge, Brave and other Chromium browsers, and in Firefox 140+.
- Permissions: access to `github.com` (to add the button) and `storage` (to remember your dashboard URL). Nothing else.
- No background script, no network requests, no analytics, no tracking.
- Keeps working through GitHub's client-side navigation, so the button shows up without page reloads.

## Install from source

### Chrome, Edge, Brave

1. Open `chrome://extensions` (or `edge://extensions`).
2. Turn on **Developer mode**.
3. Click **Load unpacked** and pick this `extension/` folder.

### Firefox

1. Open `about:debugging#/runtime/this-firefox`.
2. Click **Load Temporary Add-on…** and pick `extension/manifest.json`.

Temporary add-ons are removed when Firefox restarts. For a permanent install, use the Firefox Add-ons listing.

## Settings

Click the extension's toolbar icon, or open its **Options**, to set the dashboard URL. The default is `http://localhost:5173`. Changes apply to open GitHub tabs right away.

Running Vedocker on another machine? Forward the dashboard over SSH (`ssh -N -L 5173:127.0.0.1:5173 you@box`) and keep the default. See [Using a remote Linux box](../cmd/vedocker-mcp/README.md#using-a-remote-linux-box-ssh-tunnel).

The dashboard asks for confirmation before it deploys a repo from a URL, unless you've turned on auto-deploy.

## Development

```bash
node --test extension/test/repo.test.js   # unit tests (URL parsing, settings validation, manifest permissions)
extension/scripts/package.sh              # builds extension/dist/run-with-vedocker-<version>.zip
npx addons-linter extension/dist/run-with-vedocker-*.zip   # Mozilla's store linter
```

| File | Purpose |
|---|---|
| `manifest.json` | MV3 manifest shared by Chrome and Firefox (`browser_specific_settings` is ignored by Chrome) |
| `src/repo.js` | Pure helpers: repo detection, dashboard URL validation, link building |
| `src/content.js` | Finds the Code button and inserts the link. Re-runs on Turbo and soft-nav events and DOM changes |
| `src/content.css` | Button styling built on GitHub's Primer CSS variables, so it matches light and dark themes |
| `options/` | Settings page, also used as the toolbar popup |

Publishing to the stores: see [STORE_LISTING.md](STORE_LISTING.md). Privacy policy: [PRIVACY.md](PRIVACY.md).
