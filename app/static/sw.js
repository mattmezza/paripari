/* PariPari service worker.
   Navigations: network-first, fall back to cache (offline viewing).
   Static assets: cache-first.
   POST and anything non-GET: never touched.
   Bump CACHE when the shell changes. */
const CACHE = "paripari-v2";

const SHELL = [
  "/static/css/app.min.css",
  "/static/js/htmx.min.js",
  "/static/js/alpine.min.js",
  "/static/js/chart.umd.js",
  "/static/js/chart-sankey.min.js",
  "/static/js/app.js",
  "/static/fonts/inter-latin.woff2",
  "/static/fonts/space-grotesk-latin.woff2",
  "/static/icons/icon-192.png",
  "/static/icons/mark.svg",
  "/static/manifest.json",
  "/static/favicon-32x32.png",
];

self.addEventListener("install", (e) => {
  e.waitUntil(
    caches.open(CACHE)
      // addAll is all-or-nothing; a single 404 would break install.
      .then((c) => Promise.all(SHELL.map((u) => c.add(u).catch(() => {}))))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (e) => {
  const req = e.request;
  if (req.method !== "GET") return;           // never cache mutations
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  // Navigations (and htmx page swaps): fresh if we can, cached if we can't.
  if (req.mode === "navigate" || req.headers.get("HX-Request")) {
    e.respondWith(
      fetch(req)
        .then((res) => {
          const copy = res.clone();
          caches.open(CACHE).then((c) => c.put(req, copy));
          return res;
        })
        .catch(() => caches.match(req).then((hit) => hit || caches.match("/")))
    );
    return;
  }

  // Static: cache-first, fill on miss.
  if (url.pathname.startsWith("/static/")) {
    e.respondWith(
      caches.match(req).then((hit) =>
        hit ||
        fetch(req).then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(req, copy));
          }
          return res;
        })
      )
    );
  }
});
