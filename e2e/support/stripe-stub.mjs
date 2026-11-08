// Local stand-in for the Stripe API in the M8 e2e tests (m8-plan §14.5, §16 D24): Porto reaches it through
// PORTO_STRIPE_API_BASE (honoured only with PORTO_DEV=true), so no test touches the network. It implements what
// the Stripe driver calls (Checkout Session create/retrieve/expire, PaymentIntent retrieve/capture/cancel,
// refunds, balance, customers), a hosted pay page with "Paga" and "Annulla", and signs the webhooks it sends to
// Porto (Stripe-Signature t=…,v1=HMAC-SHA256(secret, "t.body")) with the secret the specs configure on the Stripe
// driver (STUB_WEBHOOK_SECRET). Card data never exist here: "Paga" just completes the session.
import { createHmac, randomBytes } from "node:crypto";
import { createServer } from "node:http";

const port = Number(process.env.PORTO_E2E_STRIPE_PORT ?? 18082);
const porto = (
  process.env.PORTO_E2E_BASE_URL ?? "http://127.0.0.1:18080"
).replace(/\/$/, "");
const secret =
  process.env.PORTO_E2E_STRIPE_WEBHOOK_SECRET ?? "whsec_porto_e2e_stub";
const base = `http://127.0.0.1:${port}`;

const sessions = new Map();
const intents = new Map();
const refunds = new Map();
/** Webhooks sent to Porto, for the specs: GET /__events. */
const sent = [];

const id = (prefix) => `${prefix}_${randomBytes(12).toString("hex")}`;
const now = () => Math.floor(Date.now() / 1000);

function json(res, status, body) {
  res
    .writeHead(status, { "content-type": "application/json" })
    .end(JSON.stringify(body));
}

function stripeError(res, status, type, code) {
  json(res, status, { error: { type, code, message: "stripe stub" } });
}

async function readBody(req) {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  return Buffer.concat(chunks).toString("utf8");
}

/** Stripe's bracketed form keys (metadata[marina_id]) flattened into a plain map. */
function form(raw) {
  return Object.fromEntries(new URLSearchParams(raw));
}

function metadata(f, prefix) {
  const out = {};
  for (const [k, v] of Object.entries(f)) {
    const m = new RegExp(
      `^${prefix.replace(/[[\]]/g, "\\$&")}\\[([a-z_]+)\\]$`,
    ).exec(k);
    if (m) out[m[1]] = v;
  }
  return out;
}

function intentView(pi) {
  return {
    id: pi.id,
    object: "payment_intent",
    amount: pi.amount,
    amount_received: pi.status === "succeeded" ? pi.amount : 0,
    amount_capturable: pi.status === "requires_capture" ? pi.amount : 0,
    currency: pi.currency,
    status: pi.status,
    capture_method: pi.capture_method,
    metadata: pi.metadata,
    latest_charge: pi.status === "succeeded" ? `ch_${pi.id.slice(3)}` : null,
    created: pi.created,
  };
}

function sessionView(s) {
  return {
    id: s.id,
    object: "checkout.session",
    mode: s.mode,
    status: s.status,
    payment_status: s.payment_status,
    url: s.status === "open" ? `${base}/pay/${s.id}` : null,
    expires_at: s.expires_at,
    amount_total: s.amount_total,
    currency: s.currency,
    client_reference_id: s.client_reference_id,
    customer: s.customer ?? null,
    payment_intent: s.payment_intent,
    setup_intent: null,
    metadata: s.metadata,
    success_url: s.success_url,
    cancel_url: s.cancel_url,
    created: s.created,
  };
}

/** Signs and delivers one event to Porto's payment webhook of the marina in the metadata. */
async function deliver(type, object, marinaId) {
  const event = {
    id: id("evt"),
    object: "event",
    type,
    created: now(),
    data: { object },
  };
  const body = JSON.stringify(event);
  const t = String(now());
  const v1 = createHmac("sha256", secret).update(`${t}.${body}`).digest("hex");
  const url = `${porto}/webhooks/payments/stripe/${encodeURIComponent(marinaId ?? "")}`;
  let status = 0;
  try {
    const r = await fetch(url, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "stripe-signature": `t=${t},v1=${v1}`,
      },
      body,
    });
    status = r.status;
  } catch {
    status = -1;
  }
  sent.push({ type, id: event.id, status });
  return status;
}

function escapeHtml(s) {
  return String(s).replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
}

function payPage(s) {
  const amount = (s.amount_total / 100).toFixed(2).replace(".", ",");
  return `<!doctype html>
<html lang="it">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Stripe test checkout</title></head>
<body>
<main>
<h1>Pagamento di prova</h1>
<p>Importo: <strong data-testid="stub-amount">${escapeHtml(amount)} ${escapeHtml(s.currency.toUpperCase())}</strong></p>
<form method="post" action="/pay/${escapeHtml(s.id)}"><button type="submit">Paga</button></form>
<p><a href="${escapeHtml(s.cancel_url)}">Annulla</a></p>
</main>
</body>
</html>`;
}

async function handle(req, res) {
  const url = new URL(req.url ?? "/", base);
  const path = url.pathname;
  const method = req.method ?? "GET";
  const raw = method === "GET" ? "" : await readBody(req);

  if (path === "/healthz")
    return res.writeHead(200, { "content-type": "text/plain" }).end("ok");
  if (path === "/__events") return json(res, 200, { events: sent });

  // Hosted pay page (the browser, no API key).
  let m = /^\/pay\/([\w]+)$/.exec(path);
  if (m) {
    const s = sessions.get(m[1]);
    if (!s) return res.writeHead(404).end("unknown session");
    if (method === "GET") {
      return res
        .writeHead(200, { "content-type": "text/html; charset=utf-8" })
        .end(payPage(s));
    }
    if (s.status !== "open")
      return res.writeHead(303, { location: s.success_url }).end();
    const pi = intents.get(s.payment_intent);
    s.status = "complete";
    if (pi.capture_method === "manual") {
      pi.status = "requires_capture";
      s.payment_status = "unpaid";
      await deliver(
        "checkout.session.completed",
        sessionView(s),
        s.metadata.marina_id,
      );
      await deliver(
        "payment_intent.amount_capturable_updated",
        intentView(pi),
        s.metadata.marina_id,
      );
    } else {
      pi.status = "succeeded";
      s.payment_status = "paid";
      await deliver(
        "checkout.session.completed",
        sessionView(s),
        s.metadata.marina_id,
      );
    }
    return res.writeHead(303, { location: s.success_url }).end();
  }

  // API: Bearer secret key required, like Stripe.
  if (!/^Bearer sk_(test|live)_/.test(req.headers.authorization ?? "")) {
    return stripeError(res, 401, "invalid_request_error", "api_key_invalid");
  }
  const f = form(raw);

  if (path === "/v1/balance" && method === "GET")
    return json(res, 200, { object: "balance", available: [] });
  if (path === "/v1/customers" && method === "POST")
    return json(res, 200, { id: id("cus"), object: "customer" });

  if (path === "/v1/checkout/sessions" && method === "POST") {
    const sid = id("cs_test");
    const pid = id("pi");
    const meta = metadata(f, "metadata");
    const amount = Number(f["line_items[0][price_data][unit_amount]"] ?? 0);
    const currency = f["line_items[0][price_data][currency]"] ?? "eur";
    intents.set(pid, {
      id: pid,
      amount,
      currency,
      status: "requires_payment_method",
      capture_method:
        f["payment_intent_data[capture_method]"] === "manual"
          ? "manual"
          : "automatic",
      metadata: { ...meta, ...metadata(f, "payment_intent_data[metadata]") },
      created: now(),
    });
    const s = {
      id: sid,
      mode: f.mode ?? "payment",
      status: "open",
      payment_status: "unpaid",
      expires_at: Number(f.expires_at ?? now() + 1800),
      amount_total: amount,
      currency,
      client_reference_id: f.client_reference_id ?? null,
      customer: f.customer,
      payment_intent: pid,
      metadata: meta,
      success_url: f.success_url,
      cancel_url: f.cancel_url,
      created: now(),
    };
    sessions.set(sid, s);
    return json(res, 200, sessionView(s));
  }
  m = /^\/v1\/checkout\/sessions\/([\w]+)(\/expire)?$/.exec(path);
  if (m) {
    const s = sessions.get(m[1]);
    if (!s)
      return stripeError(res, 404, "invalid_request_error", "resource_missing");
    if (m[2] && method === "POST") {
      if (s.status === "open") {
        s.status = "expired";
        await deliver(
          "checkout.session.expired",
          sessionView(s),
          s.metadata.marina_id,
        );
      }
    }
    return json(res, 200, sessionView(s));
  }

  m = /^\/v1\/payment_intents\/([\w]+)(\/capture|\/cancel)?$/.exec(path);
  if (m) {
    const pi = intents.get(m[1]);
    if (!pi)
      return stripeError(res, 404, "invalid_request_error", "resource_missing");
    if (m[2] === "/capture" && method === "POST") {
      if (pi.status !== "requires_capture") {
        return stripeError(
          res,
          400,
          "invalid_request_error",
          "payment_intent_unexpected_state",
        );
      }
      pi.status = "succeeded";
      await deliver(
        "payment_intent.succeeded",
        intentView(pi),
        pi.metadata.marina_id,
      );
    } else if (m[2] === "/cancel" && method === "POST") {
      if (pi.status === "succeeded") {
        return stripeError(
          res,
          400,
          "invalid_request_error",
          "payment_intent_unexpected_state",
        );
      }
      pi.status = "canceled";
      await deliver(
        "payment_intent.canceled",
        intentView(pi),
        pi.metadata.marina_id,
      );
    }
    return json(res, 200, intentView(pi));
  }

  if (path === "/v1/refunds" && method === "POST") {
    const pi = intents.get(f.payment_intent);
    if (!pi || pi.status !== "succeeded")
      return stripeError(
        res,
        400,
        "invalid_request_error",
        "charge_not_refundable",
      );
    const r = {
      id: id("re"),
      object: "refund",
      amount: Number(f.amount ?? pi.amount),
      currency: pi.currency,
      status: "succeeded",
      payment_intent: pi.id,
      metadata: metadata(f, "metadata"),
      created: now(),
    };
    refunds.set(r.id, r);
    setTimeout(
      () => void deliver("refund.updated", r, pi.metadata.marina_id),
      50,
    );
    return json(res, 200, r);
  }
  m = /^\/v1\/refunds\/([\w]+)$/.exec(path);
  if (m) {
    const r = refunds.get(m[1]);
    return r
      ? json(res, 200, r)
      : stripeError(res, 404, "invalid_request_error", "resource_missing");
  }

  return stripeError(res, 404, "invalid_request_error", "unknown_path");
}

createServer((req, res) => {
  handle(req, res).catch(() => {
    if (!res.headersSent) stripeError(res, 500, "api_error", "stub_failure");
  });
}).listen(port, "127.0.0.1");
