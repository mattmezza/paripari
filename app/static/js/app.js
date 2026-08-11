/* PariPari app shell behaviour. No framework, no build step. */
(function () {
  "use strict";

  /* ── Theme toggle ───────────────────────────────────────────────────────
     The blocking inline script in <head> already stamped `.dark`. This only
     handles the user flipping it. */
  function applyTheme(mode) {
    var dark = mode === "dark";
    document.documentElement.classList.toggle("dark", dark);
    document.querySelectorAll('meta[name="theme-color"]').forEach(function (m) {
      // Keep both entries; the media-matched one wins in the browser UI.
      m.setAttribute("content", getComputedStyle(document.documentElement)
        .getPropertyValue("--color-paper").trim());
    });
    document.querySelectorAll("[data-theme-toggle]").forEach(function (b) {
      b.setAttribute("aria-pressed", String(dark));
    });
  }

  document.addEventListener("click", function (e) {
    var t = e.target.closest("[data-theme-toggle]");
    if (!t) return;
    var next = document.documentElement.classList.contains("dark") ? "light" : "dark";
    try { localStorage.setItem("pp-theme", next); } catch (_) {}
    applyTheme(next);
  });

  /* ── Number tick on recalc ──────────────────────────────────────────────
     Handlers fire `pp:recalc` (HX-Trigger response header) after any change
     that moves money. Figures inside the swapped region pulse once. */
  function tick(root) {
    (root || document).querySelectorAll("[data-tick]").forEach(function (el) {
      el.classList.remove("is-ticking");
      void el.offsetWidth; // restart the animation
      el.classList.add("is-ticking");
      el.addEventListener("animationend", function () {
        el.classList.remove("is-ticking");
      }, { once: true });
    });
  }
  document.body.addEventListener("pp:recalc", function (e) { tick(e.target); });
  document.body.addEventListener("htmx:afterSwap", function (e) {
    if (e.detail.target.hasAttribute("data-tick-on-swap")) tick(e.detail.target);
  });

  /* ── Copy to clipboard ──────────────────────────────────────────────────
     The label change IS the feedback. No toast. */
  document.addEventListener("click", async function (e) {
    var btn = e.target.closest("[data-copy]");
    if (!btn) return;
    var value = btn.getAttribute("data-copy");
    try {
      await navigator.clipboard.writeText(value);
      btn.dataset.state = "done";
    } catch (_) {
      btn.dataset.state = "error";
    }
    setTimeout(function () { delete btn.dataset.state; }, 2200);
  });

  /* ── "More" sheet ───────────────────────────────────────────────────────
     Native <dialog>: focus trap, Escape, and ::backdrop come free. */
  document.addEventListener("click", function (e) {
    var open = e.target.closest("[data-sheet-open]");
    if (open) {
      e.preventDefault();
      var d = document.getElementById(open.getAttribute("data-sheet-open"));
      if (d) d.showModal();
      return;
    }
    var dlg = e.target.closest("dialog.sheet");
    if (dlg && e.target === dlg) dlg.close(); // backdrop click
    var close = e.target.closest("[data-sheet-close]");
    if (close) close.closest("dialog").close();
  });

  /* ── Service worker ─────────────────────────────────────────────────────
     ponytail: registered unconditionally; the SW itself decides what to cache. */
  if ("serviceWorker" in navigator) {
    window.addEventListener("load", function () {
      navigator.serviceWorker.register("/static/sw.js").catch(function () {});
    });
  }
})();
