// Static host page of the M8 embed tests (m8-plan §14.5): a different origin (localhost:18081) from Porto
// (127.0.0.1:18080) that embeds the booking widget through /embed.js, as a marina's own website would.
// /host.html?slug=<marina slug> picks the marina (default PORTO_E2E_SLUG). The page records every
// `porto:booking` event of the widget in window.__portoEvents and lists it, for the specs.
import { createServer } from "node:http";

const port = Number(process.env.PORTO_E2E_HOST_PORT ?? 18081);
const porto = process.env.PORTO_E2E_BASE_URL ?? "http://127.0.0.1:18080";
const defaultSlug = process.env.PORTO_E2E_SLUG ?? "e2e";

const hostPage = (slug) => `<!doctype html>
<html lang="it">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Marina host page</title>
  </head>
  <body>
    <main>
      <h1>Marina E2E</h1>
      <div id="widget" data-porto="${slug}" data-lang="it"></div>
      <ul id="events" aria-label="Eventi del widget"></ul>
    </main>
    <script>
      window.__portoEvents = [];
      document.getElementById('widget').addEventListener('porto:booking', function (e) {
        window.__portoEvents.push(e.detail);
        var li = document.createElement('li');
        li.textContent = e.detail.type + (e.detail.reference ? ' ' + e.detail.reference : ' ' + (e.detail.step || ''));
        document.getElementById('events').appendChild(li);
      });
    </script>
    <script src="${porto}/embed.js" async></script>
  </body>
</html>
`;

createServer((req, res) => {
  const url = new URL(req.url ?? "/", `http://localhost:${port}`);
  if (url.pathname === "/healthz") {
    res.writeHead(200, { "content-type": "text/plain" }).end("ok");
    return;
  }
  if (url.pathname === "/" || url.pathname === "/host.html") {
    const slug = url.searchParams.get("slug") ?? defaultSlug;
    if (!/^[a-z0-9-]{3,63}$/.test(slug)) {
      res.writeHead(400).end();
      return;
    }
    // The full page URL reaches the iframe as document.referrer, so the checkout can come back to this page
    // (return_to) with its query string.
    res
      .writeHead(200, {
        "content-type": "text/html; charset=utf-8",
        "referrer-policy": "no-referrer-when-downgrade",
      })
      .end(hostPage(slug));
    return;
  }
  res.writeHead(404).end();
}).listen(port, "localhost");
