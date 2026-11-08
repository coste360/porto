// Installable booking page (m8-plan §1.1): a shell-only service worker (public/sw.js) registered for /b/ on the
// top-level page only. Inside the widget iframe nothing is registered, and failures are ignored.
export function registerBookingServiceWorker(win: Window = window): void {
  const nav = win.navigator;
  if (!('serviceWorker' in nav)) return;
  if (win.self !== win.top) return;
  if (new URLSearchParams(win.location.search).get('embed') === '1') return;
  win.addEventListener('load', () => {
    nav.serviceWorker.register('/b/sw.js', { scope: '/b/' }).catch(() => undefined);
  });
}
