---
title: HTTPS and reverse proxy
description: Automatic Let's Encrypt certificates, or Porto behind Caddy, Nginx or Traefik.
sidebar:
  order: 7
---

Outside `localhost` Porto requires an `https` `PORTO_BASE_URL`: session cookies are `Secure` and, with an `https` URL,
Porto also sends the HSTS header. There are two options.

## Automatic certificates

With `PORTO_TLS_AUTOCERT_DOMAINS=porto.my-marina.example` Porto obtains and renews Let's Encrypt certificates itself
and listens on ports **443** (HTTPS) and **80** (redirect and ACME challenge). The domain must point to the server
and both ports must be reachable from the internet. With Docker publish `80` and `443` instead of `8080`. The
certificates are kept in the data folder.

## Behind a reverse proxy

Keep Porto on `:8080` and let the proxy terminate HTTPS. Tell Porto which addresses the proxy's requests come from, so
it can find the real client IP (used by the sign-in and request limits):

```bash
PORTO_TRUSTED_PROXIES=127.0.0.1/32,172.16.0.0/12
```

Porto reads `X-Forwarded-For` and `X-Forwarded-Proto` only from those addresses.

### Caddy

```text
porto.my-marina.example {
	reverse_proxy 127.0.0.1:8080
}
```

### Nginx

The panel receives live updates through Server-Sent Events: turn response buffering off and raise the read timeout.

```nginx
server {
    listen 443 ssl;
    server_name porto.my-marina.example;
    # ssl_certificate ... ; ssl_certificate_key ... ;

    client_max_body_size 20m;   # photo and document uploads

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_read_timeout 1h;
    }
}
```

### Traefik and platforms

With Traefik, Coolify or CapRover route the domain to port `8080` of the container; the templates in `deploy/`
already do it.

## Booking page inside another website

The public booking page can be framed only by the domains the marina authorises in the online booking settings
(`frame-ancestors` per marina). The proxy must not add its own `X-Frame-Options` or `Content-Security-Policy`, which
would override Porto's. See [Online booking](/en/gestore/prenotazione-online/#embedding-the-widget-in-your-website).
