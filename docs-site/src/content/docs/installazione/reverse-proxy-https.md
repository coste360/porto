---
title: HTTPS e reverse proxy
description: Certificati automatici Let's Encrypt oppure Porto dietro Caddy, Nginx o Traefik.
sidebar:
  order: 7
---

Fuori da `localhost` Porto richiede un `PORTO_BASE_URL` in `https`: i cookie di sessione sono `Secure` e, con un URL
`https`, Porto invia anche l'intestazione HSTS. Hai due possibilità.

## Certificati automatici

Con `PORTO_TLS_AUTOCERT_DOMAINS=porto.mio-marina.it` Porto ottiene e rinnova da sé i certificati Let's Encrypt e
ascolta sulle porte **443** (HTTPS) e **80** (redirect e verifica ACME). Il dominio deve puntare al server e le due
porte devono essere raggiungibili da internet. Con Docker pubblica `80` e `443` al posto della `8080`. I certificati
sono conservati nella cartella dati.

## Dietro un reverse proxy

Lascia Porto su `:8080` e fai terminare HTTPS al proxy. Indica a Porto da quali indirizzi arrivano le richieste del
proxy, così ricava l'IP reale del client (usato dai limiti dei tentativi e delle richieste):

```bash
PORTO_TRUSTED_PROXIES=127.0.0.1/32,172.16.0.0/12
```

Solo da questi indirizzi Porto legge `X-Forwarded-For` e `X-Forwarded-Proto`.

### Caddy

```text
porto.mio-marina.it {
	reverse_proxy 127.0.0.1:8080
}
```

### Nginx

Il pannello riceve gli aggiornamenti in tempo reale con Server-Sent Events: disattiva il buffering delle risposte e
alza il timeout di lettura.

```nginx
server {
    listen 443 ssl;
    server_name porto.mio-marina.it;
    # ssl_certificate ... ; ssl_certificate_key ... ;

    client_max_body_size 20m;   # caricamento di foto e documenti

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

### Traefik e piattaforme

Con Traefik, Coolify o CapRover basta instradare il dominio alla porta `8080` del container; i modelli in `deploy/`
lo fanno già.

## Pagina di prenotazione in un altro sito

La pagina pubblica di prenotazione può essere incorporata in un iframe solo dai domini che il gestore autorizza nelle
impostazioni della prenotazione online (`frame-ancestors` per marina). Il proxy non deve aggiungere un proprio
`X-Frame-Options` o `Content-Security-Policy`, che prevarrebbero su quelli di Porto. Vedi
[Prenotazione online](/gestore/prenotazione-online/#incorporare-il-widget-nel-sito).
