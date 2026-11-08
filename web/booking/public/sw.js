/* global self, caches */
/* Shell-only service worker of the booking pages (m8-plan §1.1), scope /b/. It never touches the API, never
 * caches API answers, tokens or personal data: page navigations go to the network first and fall back to the last
 * page shell when offline; the hashed, immutable assets under /b/assets/ are cached on first use. The start URL
 * comes from the page that was installed (the manifest has no start_url), so each marina installs its own page. */
const CACHE = 'porto-booking-shell-v1';
const SHELL = '/b/__shell';

self.addEventListener('install', () => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((k) => k.startsWith('porto-booking-') && k !== CACHE)
            .map((k) => caches.delete(k)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin || !url.pathname.startsWith('/b/')) return;

  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req)
        .then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(CACHE).then((c) => c.put(SHELL, copy));
          }
          return res;
        })
        .catch(() =>
          caches
            .open(CACHE)
            .then((c) => c.match(SHELL))
            .then((r) => r || Response.error()),
        ),
    );
    return;
  }

  if (url.pathname.startsWith('/b/assets/')) {
    event.respondWith(
      caches.open(CACHE).then((c) =>
        c.match(req).then(
          (hit) =>
            hit ||
            fetch(req).then((res) => {
              if (res.ok) c.put(req, res.clone());
              return res;
            }),
        ),
      ),
    );
  }
});
