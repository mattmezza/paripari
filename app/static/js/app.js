/* PariPari app shell behaviour. No framework, no build step. */
(function () {
  "use strict";

  /* ── htmx settle vs Alpine ──────────────────────────────────────────────
     htmx "settles" class/style/width/height on any swapped-in element whose id
     matches one already on the page: the new element first wears the OLD
     element's values, then htmx restores the server-rendered ones a beat later.
     Alpine's x-show writes `display:none` as an inline style, so that restore
     wipes it — and x-show only re-runs when its expression changes, so the
     element stays visible. That is why a boosted navigation to a saved
     projection arrived with the edit-name field (and the save form) hanging
     open. Nothing here animates between old and new attribute values, so the
     whole settle step is dead weight: turn it off. */
  if (window.htmx) window.htmx.config.attributesToSettle = [];

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

  /* ── Privacy blur ───────────────────────────────────────────────────────
     `.private` on <html> blurs every figure (CSS owns the selector list);
     `.revealed` lifts it for pp-private-timer seconds. The blocking head
     script already stamped `.private`, so there is no flash of amounts.
     Preferences are per-device on purpose — this is about who is standing
     behind *this* screen — so they live in localStorage, not the household. */
  var revealTimer = null;

  function ls(key, fallback) {
    try { var v = localStorage.getItem(key); return v === null ? fallback : v; }
    catch (_) { return fallback; }
  }
  function lsSet(key, value) { try { localStorage.setItem(key, value); } catch (_) {} }

  function syncPrivateButtons() {
    var on = document.documentElement.classList.contains("private");
    document.querySelectorAll("[data-privacy-toggle]").forEach(function (b) {
      b.setAttribute("aria-pressed", String(on));
    });
  }

  function conceal() {
    clearTimeout(revealTimer);
    document.documentElement.classList.remove("revealed");
  }

  function reveal() {
    clearTimeout(revealTimer);
    document.documentElement.classList.add("revealed");
    var secs = parseInt(ls("pp-private-timer", "10"), 10);
    // 0 means "stay revealed until I say otherwise".
    if (secs > 0) revealTimer = setTimeout(conceal, secs * 1000);
  }

  function setPrivate(on) {
    document.documentElement.classList.toggle("private", on);
    conceal();
    lsSet("pp-private", on ? "1" : "0");
    syncPrivateButtons();
  }

  window.ppPrivacy = { set: setPrivate, reveal: reveal, conceal: conceal };
  syncPrivateButtons();

  document.addEventListener("click", function (e) {
    if (e.target.closest("[data-privacy-toggle]")) {
      setPrivate(!document.documentElement.classList.contains("private"));
      return;
    }
    if (!document.documentElement.classList.contains("private")) return;
    // Clicking a blurred figure reveals everything; clicking again hides it.
    if (e.target.closest(".figure, .tnum, td.num, .input--figure, [data-tick], canvas")) {
      if (document.documentElement.classList.contains("revealed")) conceal();
      else reveal();
    }
  });

  /* ── Shake to reveal (mobile) ───────────────────────────────────────────
     ponytail: a plain magnitude threshold with a cooldown, no smoothing or
     axis analysis. iOS needs an explicit permission grant, which settings
     asks for from a real tap. */
  var lastShake = 0;
  window.ppShake = function (enabled) {
    lsSet("pp-shake", enabled ? "1" : "0");
    if (!enabled || !window.DeviceMotionEvent) return Promise.resolve(enabled);
    var ask = DeviceMotionEvent.requestPermission
      ? DeviceMotionEvent.requestPermission()
      : Promise.resolve("granted");
    return ask.then(function (state) {
      if (state !== "granted") { lsSet("pp-shake", "0"); return false; }
      return true;
    }).catch(function () { lsSet("pp-shake", "0"); return false; });
  };

  window.addEventListener("devicemotion", function (e) {
    if (ls("pp-shake", "0") !== "1") return;
    var a = e.accelerationIncludingGravity;
    if (!a) return;
    var force = Math.abs(a.x || 0) + Math.abs(a.y || 0) + Math.abs(a.z || 0);
    if (force < 35) return;
    var now = e.timeStamp || 0;
    if (now - lastShake < 1200) return;
    lastShake = now;
    if (!document.documentElement.classList.contains("private")) setPrivate(true);
    else if (document.documentElement.classList.contains("revealed")) conceal();
    else reveal();
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
    syncPrivateButtons(); // boosted navigation brings a fresh, unsynced nav
  });

  /* ── Validation errors (422) ────────────────────────────────────────────
     Handlers answer invalid forms with 422 + the re-rendered form (or an
     inline .helper--error via HX-Retarget). htmx ignores non-2xx by default,
     so without this the user sees nothing at all. Any HX-Retarget/HX-Reswap
     the response asked for is already applied by the time this fires. */
  document.body.addEventListener("htmx:beforeSwap", function (e) {
    var d = e.detail;
    if (d.xhr.status !== 422) return;
    d.shouldSwap = true;
    d.isError = false;
    // When the response is a re-render of an element already on the page (same
    // id on its root), swap it in place instead of into the success path's
    // target — the expenses form, for one, targets the list it lives inside,
    // and swapping a form over the list would delete the list.
    var tpl = document.createElement("template");
    tpl.innerHTML = d.serverResponse;
    var root = tpl.content.firstElementChild;
    var here = root && root.id && document.getElementById(root.id);
    if (here) {
      d.target = here;
      d.swapOverride = "outerHTML";
    }
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
