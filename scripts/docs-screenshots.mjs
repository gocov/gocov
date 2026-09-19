// Regenerates the screenshots under docs/assets from the preview harness, so
// the docs show the UI as it is rather than as it was. Drives headless Chrome
// over the DevTools protocol; Node 22+ (global WebSocket and fetch), no
// dependencies.
//
//   cd web && npm run build && cd ..          # the preview serves the embedded bundle
//   GOCOV_PREVIEW_AUTH=1 go run ./cmd/gocov-preview &
//   node scripts/docs-screenshots.mjs
//
// Start from a FRESH preview: the shots rely on its seeded state (the two-part
// upload, the broken GitHub workspace the dev user owns, the connected one
// with no report yet). CHROME and GOCOV_PREVIEW override the browser binary
// and the server address. Light theme, 1280px wide, 2x.
import { spawn } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const BASE = process.env.GOCOV_PREVIEW ?? "http://localhost:8099";
const OUT = fileURLToPath(new URL("../docs/assets", import.meta.url));
const CHROME = process.env.CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const PORT = 9333;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const chrome = spawn(CHROME, [
  "--headless=new", `--remote-debugging-port=${PORT}`, `--user-data-dir=${mkdtempSync(join(tmpdir(), "gocov-shots-"))}`,
  "--hide-scrollbars", "--force-color-profile=srgb", "--no-first-run", "--no-default-browser-check", "about:blank",
], { stdio: "ignore" });

async function target() {
  for (let i = 0; i < 50; i++) {
    try {
      const list = await (await fetch(`http://127.0.0.1:${PORT}/json`)).json();
      const page = list.find((t) => t.type === "page");
      if (page) return page.webSocketDebuggerUrl;
    } catch {}
    await sleep(200);
  }
  throw new Error("chrome did not start");
}

const ws = new WebSocket(await target());
await new Promise((r) => ws.addEventListener("open", r, { once: true }));
let seq = 0;
const pending = new Map();
ws.addEventListener("message", (e) => {
  const msg = JSON.parse(e.data);
  if (msg.id && pending.has(msg.id)) {
    const { resolve, reject } = pending.get(msg.id);
    pending.delete(msg.id);
    msg.error ? reject(new Error(msg.error.message)) : resolve(msg.result);
  }
});
const send = (method, params = {}) =>
  new Promise((resolve, reject) => {
    const id = ++seq;
    pending.set(id, { resolve, reject });
    ws.send(JSON.stringify({ id, method, params }));
  });

const evaluate = async (expression) => {
  const r = await send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true });
  if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description ?? "evaluate failed");
  return r.result.value;
};

async function viewport(width, height) {
  await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 2, mobile: false });
}

async function go(path, waitFor) {
  await send("Page.navigate", { url: BASE + path });
  for (let i = 0; i < 100; i++) {
    await sleep(150);
    if (await evaluate(`!!(${waitFor})`).catch(() => false)) break;
    if (i === 99) throw new Error(`timed out waiting on ${path} for ${waitFor}`);
  }
  await sleep(500); // fonts, the last fetch, the first paint after it
}

/** Clip in CSS px; `rect` may be a selector expression returning an element. */
async function shoot(name, clip) {
  const r = await send("Page.captureScreenshot", { format: "png", captureBeyondViewport: true, clip: { ...clip, scale: 1 } });
  writeFileSync(join(OUT, name), Buffer.from(r.data, "base64"));
  console.log(name, Math.round(clip.width * 2) + "x" + Math.round(clip.height * 2));
}

const rectOf = (expr, pad = 0) =>
  evaluate(`(() => { const el = ${expr}; const r = el.getBoundingClientRect(); return { x: Math.max(0, r.left + scrollX - ${pad}), y: Math.max(0, r.top + scrollY - ${pad}), width: r.width + ${pad * 2}, height: r.height + ${pad * 2} }; })()`);

/** From the top of the page down to the bottom of `expr`, plus breathing room. */
const pageDownTo = async (expr, pad = 32) => {
  const bottom = await evaluate(`(() => { const r = (${expr}).getBoundingClientRect(); return r.bottom + scrollY; })()`);
  return { x: 0, y: 0, width: 1280, height: Math.ceil(bottom + pad) };
};

try {
  await send("Page.enable");
  await send("Runtime.enable");
  await send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "light" }] });
  await viewport(1280, 900);

  // 1. Signed out: the login page.
  await go("/login", `document.querySelector('a[href^="/oauth/"]')`);
  await shoot("login.png", { x: 0, y: 0, width: 1280, height: 697 });

  // Sign in through the preview's bounce-back provider.
  await go("/oauth/bitbucket/start?next=%2Fw%2Fbitbucket%2Facme", `document.querySelector('table')`);

  // 2. Dashboard.
  await shoot("dashboard.png", await pageDownTo(`document.querySelector('main').lastElementChild`, 40));

  // 3–4. Repo page: the verdict and the trend.
  await go("/repos/bitbucket/acme/widgets", `document.querySelector('.TrendChart')`);
  await shoot("gate-verdict.png", await rectOf(`(document.querySelector('.VerdictCard').closest('.Card') ?? document.querySelector('.VerdictCard'))`, 12));
  await shoot("trend.png", await rectOf(`(document.querySelector('.TrendChart').closest('.Card') ?? document.querySelector('.TrendChart'))`, 6));

  // 5–6. The two-part upload and a source file with newly uncovered lines.
  const upload = await evaluate(`[...document.querySelectorAll('a')].map(a => a.getAttribute('href')).find(h => /^\\/uploads\\/\\d+$/.test(h || ''))`);
  await go(upload, `document.querySelector('.ProvenanceCard')`);
  const parts = await evaluate(`/merged from/i.test(document.querySelector('.ProvenanceCard').innerText)`);
  console.log("upload", upload, "mentions merged parts:", parts);
  await shoot("upload-parts.png", await rectOf(`(document.querySelector('.ProvenanceCard').closest('.Card') ?? document.querySelector('.ProvenanceCard'))`, 12));

  const file = await evaluate(`[...document.querySelectorAll('a')].map(a => a.getAttribute('href')).find(h => /billing\\/charge\\.go$/.test(h || '')) ?? [...document.querySelectorAll('a')].map(a => a.getAttribute('href')).find(h => /\\/files\\//.test(h || ''))`);
  await go(file, `document.querySelector('.SourceViewer')`);
  await shoot("source-view.png", { x: 0, y: 0, width: 1280, height: 900 });

  // 7. Reporting broken: the settings card of a workspace whose grant was revoked.
  // Signed in through GitHub, as an owner, so the card offers the reinstall.
  await go("/oauth/github/start?next=%2Fworkspace-settings%2Fgithub%2Fgh-broken", `document.querySelector('#reporting')`);
  await shoot("reporting-broken.png", await rectOf(`document.querySelector('#reporting')`, 16));

  // 8. The setup card on the dashboard of a connected GitHub workspace with no
  // report yet: the tokenless snippet, which is what most people will see.
  await evaluate(`localStorage.setItem('gocov.setup.language', 'go')`);
  await go("/w/github/gh-connected", `document.querySelector('.SetupChecklist pre')`);
  await shoot("onboarding.png", await pageDownTo(`document.querySelector('.SetupChecklist')`, 32));
} finally {
  ws.close();
  chrome.kill();
}
