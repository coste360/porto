// Service worker of the admin panel (m6b-plan §3, D8). Hand-written, no imports (built as a separate entry to
// dist/sw.js, no hash): it caches the shell only and never API data, which lives in IndexedDB.
//
//   install   precache the shell (list injected by scripts/sw-precache.mjs) into porto-shell-<build>
//   activate  delete older porto-shell-* caches; the new worker waits until the page asks it to take over
//   fetch     navigations under /admin/: network first (3 s), then the cached index.html;
//             hashed assets: cache first; everything else under /admin/ (icons, manifest): cache, then network;
//             /api/*, /healthz, /readyz, the SSE stream and signed file links are never intercepted.

type Precache = { version: string; build: string; urls: string[] };

type ExtendableEventLike = Event & { waitUntil(p: Promise<unknown>): void };
type FetchEventLike = ExtendableEventLike & {
  request: Request;
  respondWith(r: Response | Promise<Response>): void;
};
type MessageEventLike = Event & { data: unknown };
type WorkerScope = {
  location: Location;
  registration: { scope: string };
  skipWaiting(): Promise<void>;
  clients: { claim(): Promise<void> };
  addEventListener(type: 'install' | 'activate', fn: (e: ExtendableEventLike) => void): void;
  addEventListener(type: 'fetch', fn: (e: FetchEventLike) => void): void;
  addEventListener(type: 'message', fn: (e: MessageEventLike) => void): void;
};

const sw = self as unknown as WorkerScope;
// Written as `self.__PORTO_PRECACHE__` so that the placeholder survives minification (scripts/sw-precache.mjs).
const PRECACHE: Precache = (self as unknown as { __PORTO_PRECACHE__?: Precache })
  .__PORTO_PRECACHE__ ?? {
  version: 'dev',
  build: 'dev',
  urls: [],
};
const PREFIX = 'porto-shell-';
const CACHE = `${PREFIX}${PRECACHE.build}`;
const NAVIGATION_TIMEOUT_MS = 3_000;

const scopePath = new URL(sw.registration.scope).pathname; // "/admin/"
const indexUrl = `${scopePath}index.html`;

/** Requests the worker never touches: the browser handles them as if there were no worker. */
function bypass(url: URL, origin: string): boolean {
  if (url.origin !== origin) return true;
  const p = url.pathname;
  if (p.startsWith('/api/') || p === '/healthz' || p === '/readyz' || p.startsWith('/lan/'))
    return true;
  if (url.searchParams.has('sig') || url.searchParams.has('token')) return true; // signed links
  return !p.startsWith(scopePath);
}

sw.addEventListener('install', (e) => {
  e.waitUntil(
    caches.open(CACHE).then((c) => c.addAll(PRECACHE.urls.length ? PRECACHE.urls : [indexUrl])),
  );
});

sw.addEventListener('activate', (e) => {
  e.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys.filter((k) => k.startsWith(PREFIX) && k !== CACHE).map((k) => caches.delete(k)),
        ),
      )
      .then(() => sw.clients.claim()),
  );
});

sw.addEventListener('message', (e) => {
  const d = e.data as { type?: string } | null;
  if (d?.type === 'SKIP_WAITING') void sw.skipWaiting();
  if (d?.type === 'GET_BUILD') {
    const port = (e as unknown as { ports?: MessagePort[] }).ports?.[0];
    port?.postMessage({ version: PRECACHE.version, build: PRECACHE.build });
  }
});

async function networkFirst(req: Request): Promise<Response> {
  const cache = await caches.open(CACHE);
  const timeout = new Promise<null>((res) => setTimeout(() => res(null), NAVIGATION_TIMEOUT_MS));
  try {
    const res = await Promise.race([fetch(req), timeout]);
    if (res && res.ok) return res;
    if (res) return (await cache.match(indexUrl)) ?? res;
  } catch {
    // offline: fall back to the shell
  }
  return (await cache.match(indexUrl)) ?? Response.error();
}

async function cacheFirst(req: Request): Promise<Response> {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  if (res.ok && req.method === 'GET') void cache.put(req, res.clone());
  return res;
}

sw.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (bypass(url, sw.location.origin)) return;
  if (req.mode === 'navigate') {
    e.respondWith(networkFirst(req));
    return;
  }
  e.respondWith(cacheFirst(req));
});
