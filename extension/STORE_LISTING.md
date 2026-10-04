# Store listing and submission guide

The same zip is uploaded to both stores. Build it from the repo root:

```bash
extension/scripts/package.sh
# -> extension/dist/run-with-vedocker-0.1.0.zip
```

Bump `version` in `manifest.json` before every new upload. Both stores reject a version they've already seen.

---

## Listing copy

**Name** (both stores)

```
Run with Vedocker
```

**Short description / summary** (Chrome: 132 characters max, taken from the manifest. Firefox summary: 250 max)

```
Adds a Run with Vedocker button to GitHub repos that opens the repo in your own Vedocker dashboard to build and run it.
```

**Detailed description** (both stores)

```
Found a repo on GitHub and want to see it running? Click Run with Vedocker.

The extension adds one button next to GitHub's green Code button. Clicking it opens the repo in your own Vedocker dashboard, which clones it, builds it and starts it as a container. If the repo has no Dockerfile, Vedocker's AI writes one.

Vedocker is an open-source container platform built from scratch in Go on Linux namespaces and cgroups. You run it yourself, on your own machine or server. This extension is a shortcut into your own dashboard. It doesn't send code or data to any third party.

FEATURES
• One-click "Run with Vedocker" button on every GitHub repo page
• Matches GitHub's look in light and dark themes
• Works with GitHub's instant page navigation, no reloads needed
• Set your dashboard URL (default http://localhost:5173) from the toolbar icon

PRIVACY
• Only two permissions: access to github.com (to add the button) and storage (to remember your dashboard URL)
• No analytics, no tracking, no background script, no network requests
• Open source: https://github.com/Vedthakar/Vedocker

REQUIREMENTS
You need a running Vedocker dashboard. Setup takes a few minutes: https://github.com/Vedthakar/Vedocker#quickstart

Vedocker asks you to confirm before it deploys a repo from a link, because deployed repos run as root on your machine.
```

**Category**: Chrome: *Developer Tools*. Firefox: *Web Development*.

**Homepage / support URL**: `https://github.com/Vedthakar/Vedocker`

**Support / issues URL**: `https://github.com/Vedthakar/Vedocker/issues`

**Privacy policy URL**: `https://github.com/Vedthakar/Vedocker/blob/main/extension/PRIVACY.md`

**License** (Firefox asks): MIT

---

## Images you need

All screenshots at **1280×800** PNG or JPEG. Both stores accept that size. Use a clean browser profile, hide other extensions' icons and bookmarks, and zoom the page to 100%.

| # | Required by | What to capture |
|---|---|---|
| 1 | Chrome (≥1), Firefox (recommended) | **Hero.** A popular repo's GitHub page (light theme) with the **Run with Vedocker** button next to the green Code button. Use a window at least 1280 wide so the button shows its label |
| 2 | — | Same page in **GitHub dark theme**, so people see it fits in |
| 3 | — | **Right after clicking.** The Vedocker dashboard at `localhost:5173/github.com/owner/repo` with the deploy confirmation or build logs streaming |
| 4 | — | **It's running.** The dashboard container list showing the new container as running, with its logs |
| 5 | — | **Settings.** The toolbar popup open over a GitHub page, showing the dashboard URL field |
| — | Chrome (**required**) | **Small promo tile, 440×280.** Draft included: [`store/promo-small-440x280.png`](store/promo-small-440x280.png) |
| — | Chrome (optional) | **Marquee promo tile, 1400×560.** Only used if the store features the extension |
| — | Both | **Icon.** 128×128 is already in `icons/icon-128.png`. Chrome uses it as the store icon. Firefox reads it from the zip |

Tip: screenshots 1 and 3 side by side (before/after the click) make a strong first impression in both stores.

---

## Chrome Web Store: step by step

1. **Register.** Go to https://chrome.google.com/webstore/devconsole, sign in with the Google account that should own the listing, and pay the one-time $5 registration fee. Verify the contact email.
2. **Upload.** Click **New item** and upload `extension/dist/run-with-vedocker-<version>.zip`.
3. **Store listing tab.**
   - Description: paste the detailed description above.
   - Category: Developer Tools. Language: English.
   - Store icon: `extension/icons/icon-128.png`.
   - Screenshots: upload 1 to 5 from the table above.
   - Small promo tile: `extension/store/promo-small-440x280.png`.
   - Homepage URL and Support URL: from the copy above.
4. **Privacy tab.**
   - **Single purpose:** `Adds a button to GitHub repository pages that opens the repository in the user's self-hosted Vedocker dashboard.`
   - **Permission justification, storage:** `Stores the one setting the user enters: the URL of their Vedocker dashboard.`
   - **Permission justification, host permission (https://github.com/\*):** `The content script adds the "Run with Vedocker" button next to the Code button on GitHub repository pages. It runs on no other site.`
   - **Remote code:** No, I am not using remote code.
   - **Data usage:** leave every data type unchecked. Tick all three certifications (no selling data, no unrelated use, no creditworthiness use).
   - **Privacy policy URL:** from the copy above.
5. **Distribution tab.** Public, all regions. Free.
6. **Submit for review.** Click **Submit for review**. Narrow permissions and no remote code usually mean a review of a few days. You'll get an email when it's approved. Then add the store link to the README.

Updates: bump `version`, rebuild the zip, open the item → **Package** → **Upload new package** → **Submit for review**.

---

## Firefox Add-ons (AMO): step by step

1. **Account.** Sign in at https://addons.mozilla.org/developers/ with a Firefox account and accept the developer agreement.
2. **Submit.** Click **Submit a New Add-on**, choose **On this site** (listed on AMO), and upload the same zip. The automatic validator should report 0 errors and 0 warnings. That matches `npx addons-linter`.
3. **Source code.** When asked whether the add-on uses a minifier, bundler or code generator, answer **No**. The zip is plain, unminified source, so no separate source upload is needed.
4. **Describe the add-on.**
   - Name and summary: from the copy above.
   - Description: the detailed description above.
   - Categories: Web Development.
   - Support email or website: the issues URL. Homepage: the repo URL.
   - License: MIT.
   - Privacy policy: paste the contents of `PRIVACY.md`, or link to it.
5. **Notes to reviewer** (helps it pass quickly):
   ```
   The extension adds a link next to the Code button on GitHub repository pages. To test: open https://github.com/Vedthakar/Vedocker. A "Run with Vedocker" button appears next to "Code" and links to http://localhost:5173/github.com/Vedthakar/Vedocker. A local Vedocker dashboard is not needed to verify the link. The toolbar icon opens the settings popup where the dashboard URL can be changed. No background script, no network requests, no data collection. data_collection_permissions is set to "none".
   ```
6. **Screenshots.** After submitting, open the listing's **Edit Product Page** → **Images** and upload the screenshots (1280×800). The icon comes from the zip.
7. **Wait for review.** Listed add-ons are signed right away and reviewed by humans afterwards, often within a day or two. You'll get an email.

Updates: bump `version`, rebuild, open the add-on → **Upload New Version**.

---

## After approval

- Add the store links to the main README's extension section, replacing the "install from source" note.
- Pin a short "how it works" GIF on both listings: GitHub page → click → container running.
