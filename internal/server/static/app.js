// Micro-interactions that htmx does not cover. Keep this file tiny.

// Banner dismissal: <button data-dismiss="element-id"> hides the element
// for the rest of the browser session (matching the "dismissable per
// session" rule for the open-UI auth banner).
document.querySelectorAll("[data-dismiss]").forEach((btn) => {
  const el = document.getElementById(btn.getAttribute("data-dismiss"));
  if (!el) return;
  const key = "dismissed:" + el.id;
  if (sessionStorage.getItem(key)) el.remove();
  btn.addEventListener("click", () => {
    sessionStorage.setItem(key, "1");
    el.remove();
  });
});

// Destructive-action confirmation: <form data-confirm="message"> asks
// before submitting (e.g. workspace token rotation).
document.addEventListener("submit", (e) => {
  const msg = e.target.getAttribute && e.target.getAttribute("data-confirm");
  if (msg && !window.confirm(msg)) e.preventDefault();
});

// Reveal-a-secret: <button data-reveal="#selector"> swaps a masked value for
// its full form (kept in the target's data-full) and back. Used by the
// onboarding upload-token card so the token is not shown by default.
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-reveal]");
  if (!btn) return;
  const el = document.querySelector(btn.getAttribute("data-reveal"));
  if (!el || el.dataset.full === undefined) return;
  if (el.dataset.shown) {
    delete el.dataset.shown;
    el.textContent = el.dataset.masked;
    btn.textContent = "Reveal";
  } else {
    el.dataset.masked = el.textContent;
    el.dataset.shown = "1";
    el.textContent = el.dataset.full;
    btn.textContent = "Hide";
  }
});

// Coverage-gate toggle: the .tg switch enables/disables its paired number
// input. A disabled input is not submitted, so the server reads an empty
// value and treats that gate as off — the same "empty = off" contract the
// settings handler already uses. The header chip tracks how many are on.
document.addEventListener("click", (e) => {
  const tg = e.target.closest(".tg");
  if (!tg) return;
  const gate = tg.closest(".gate");
  const input = gate && gate.querySelector(".gate-input");
  const on = tg.getAttribute("aria-pressed") !== "true";
  tg.setAttribute("aria-pressed", String(on));
  if (on) {
    gate.removeAttribute("data-off");
    if (input) { input.disabled = false; input.focus(); }
  } else {
    gate.setAttribute("data-off", "");
    if (input) input.disabled = true;
  }
  const chip = document.getElementById("gatecount");
  if (chip) {
    const n = document.querySelectorAll(".gate:not([data-off])").length;
    chip.textContent = n === 0 ? "None active" : n + " active";
  }
});

// Settings sidebar: highlight the section link that was clicked.
document.addEventListener("click", (e) => {
  const link = e.target.closest(".side a");
  if (!link) return;
  document.querySelectorAll(".side a").forEach((a) => a.classList.remove("on"));
  link.classList.add("on");
});

// Copy-to-clipboard: <button data-copy="#selector">Copy</button> copies the
// value/text of the referenced element. Falls back to select+execCommand on
// plain-http deployments where the Clipboard API is unavailable.
document.addEventListener("click", (e) => {
  const btn = e.target.closest("[data-copy]");
  if (!btn) return;
  const src = document.querySelector(btn.getAttribute("data-copy"));
  if (!src) return;
  const text = src.value !== undefined ? src.value : src.textContent;

  const done = () => {
    const old = btn.textContent;
    btn.textContent = "Copied!";
    btn.disabled = true;
    setTimeout(() => { btn.textContent = old; btn.disabled = false; }, 1200);
  };

  const legacyCopy = () => {
    if (!src.select) return;
    src.select();
    document.execCommand("copy");
    done();
  };
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(done, legacyCopy);
  } else {
    legacyCopy();
  }
});
