const VERSION = "grocerly-shell-v2";
const SHELL = ["/", "/pos", "/login", "/sync", "/manifest.webmanifest"];
self.addEventListener("install", event => { event.waitUntil(caches.open(VERSION).then(cache => cache.addAll(SHELL)).then(() => self.skipWaiting())); });
self.addEventListener("activate", event => { event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(key => key !== VERSION).map(key => caches.delete(key)))).then(() => self.clients.claim())); });
self.addEventListener("fetch", event => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;
  if (request.mode === "navigate") {
    event.respondWith(fetch(request).then(response => { if (response.ok) caches.open(VERSION).then(cache => cache.put(request, response.clone())); return response; }).catch(async () => (await caches.match(request)) || (url.pathname.startsWith("/pos") ? await caches.match("/pos") : await caches.match("/"))));
    return;
  }
  if (url.pathname.startsWith("/_next/static/") || url.pathname.match(/\.(?:css|js|woff2|png|svg|ico)$/)) {
    event.respondWith(caches.match(request).then(hit => hit || fetch(request).then(response => { if (response.ok) caches.open(VERSION).then(cache => cache.put(request, response.clone())); return response; })));
  }
});
