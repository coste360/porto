---
title: Configurazione
description: Le variabili d'ambiente PORTO_* del server e dell'app desktop.
sidebar:
  order: 5
---

Tutta la configurazione del server avviene tramite variabili d'ambiente, documentate anche in
[`.env.example`](https://github.com/coste360/porto/blob/main/.env.example). Porto controlla tutte le variabili
all'avvio e, se qualcosa non va, elenca gli errori indicando il nome della variabile, mai il valore.

## Server

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_ADDR` | `:8080` | Indirizzo di ascolto |
| `PORTO_BASE_URL` | — (obbligatoria) | URL pubblico usato nei link e nei cookie; `https` obbligatorio fuori da `localhost` e dalla modalità sviluppo |
| `PORTO_DATA_DIR` | `./data` (`/data` nell'immagine) | Database SQLite `porto.db`, file cifrati, certificati, backup |
| `DATABASE_URL` | vuoto | Con `postgres://` o `postgresql://` usa PostgreSQL al posto di SQLite |
| `PORTO_SECRET_KEY` | — (obbligatoria) | Chiave di cifratura, 32 byte in base64 (`openssl rand -base64 32`). Più chiavi separate da virgola per la rotazione: la prima è attiva |
| `PORTO_AUTO_MIGRATE` | `true` | Applica le migrazioni all'avvio di `serve`; con `false` il server non parte con migrazioni in sospeso |
| `PORTO_TLS_AUTOCERT_DOMAINS` | vuoto | Domini per i certificati Let's Encrypt automatici (ascolto su 443 e 80) |
| `PORTO_TRUSTED_PROXIES` | vuoto | CIDR dei reverse proxy fidati, da cui leggere `X-Forwarded-For` e `X-Forwarded-Proto` |

## Email

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_SMTP_URL` | vuoto | `smtp://utente:password@host:587` (STARTTLS) o `smtps://utente:password@host:465` |
| `PORTO_MAIL_FROM` | `Porto <noreply@localhost>` | Mittente; con SMTP deve usare un dominio reale |
| `PORTO_MAIL_DIR` | vuoto | Senza SMTP, cartella in cui scrivere le email come file `.eml` (sviluppo e test); vuota = solo log |

Porto non impone alcun provider: usa il server SMTP che preferisci.

## Sicurezza e limiti

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_REQUIRE_TOTP_FOR_OWNERS` | `false` | Autenticazione a due fattori obbligatoria per titolari e amministrazione |
| `PORTO_PUBLIC_READS_PER_MINUTE` | `120` | Richieste al minuto per rete di provenienza sull'API pubblica (1–10000) |
| `PORTO_LOGIN_ATTEMPTS_PER_MINUTE` | `30` | Tentativi di accesso al minuto per indirizzo (1–30 in produzione); il limite di 5 errori per account in 15 minuti è fisso |
| `PORTO_HOLD_TTL` | `10m` | Durata del blocco temporaneo di un posto durante una prenotazione online (1m–60m) |
| `PORTO_SENSOR_RAW_RETENTION` | `30d` | Conservazione delle letture grezze dei sensori (1d–3650d), escluse dal backup |

## Log

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `PORTO_LOG_FORMAT` | `json` (`text` in sviluppo) | Formato dei log |
| `PORTO_TELEMETRY` | `false` | Porto non ha telemetria: il valore deve restare `false` |

## Primo utente e demo

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_BOOTSTRAP_OWNER_EMAIL`, `PORTO_BOOTSTRAP_OWNER_NAME`, `PORTO_BOOTSTRAP_OWNER_ORG` | vuoto | Titolare e organizzazione creati all'avvio, solo se il database non ha utenti (tutte e tre più `PORTO_ADMIN_PASSWORD`, oppure nessuna) |
| `PORTO_ADMIN_PASSWORD` | vuoto | Password per `porto admin create`, il titolare iniziale e `porto seed demo`; mai scritta nei log |
| `PORTO_DEMO_PAIRING_USER`, `PORTO_DEMO_PAIRING_MARINA` | vuoto | Solo istanze dimostrative: utente e slug del marina per l'abbinamento dimostrativo dei dispositivi (entrambe o nessuna) |
| `PORTO_DEMO_INSTANCE` | vuoto | Solo per `porto seed demo --reset` su un'istanza dimostrativa pubblica ([Dati dimostrativi](/installazione/dati-demo/)); mai in produzione |

## Sviluppo e test

Da non usare in produzione.

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_DEV` | `false` | Modalità sviluppo: chiave generata in `PORTO_DATA_DIR/secret.key`, cookie senza `Secure` su `http`, log testuali |
| `PORTO_WEBHOOKS_ALLOW_PRIVATE` | `false` | Webhook verso indirizzi privati e `http` |
| `PORTO_DEV_FAKE_CHANNEL`, `PORTO_STRIPE_API_BASE` | `false`, vuoto | Solo con `PORTO_DEV=true`: canale di prenotazione finto e API Stripe finta per i test |
| `PORTO_UPDATE_URL` | vuoto | App desktop, solo con `PORTO_DEV=true`: URL del manifest della versione al posto di quello di Coste360 |
| `PORTO_DESKTOP_LAN_LOOPBACK` | `false` | App desktop, solo con `PORTO_DEV=true`: la rete locale ascolta su `127.0.0.1` (test) |
| `PORTO_DESKTOP_FIRST_RUN_FILE` | `false` | App desktop, solo con `PORTO_DEV=true`: scrive in un file della cartella dati l'URL del primo avvio (test) |

## File compose

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_VERSION` | `latest` | Tag dell'immagine `ghcr.io/coste360/porto` (`X.Y` per ricevere le correzioni, `X.Y.Z` per restare fermi) |
| `PORTO_PUBLISH_PORT` | `8080` | Porta pubblicata sull'host |
| `POSTGRES_PASSWORD` | — | Solo `deploy/docker-compose.postgres.yml` (obbligatoria lì) |

## App desktop

L'app desktop imposta da sé indirizzo, URL, cartella dati e chiave segreta (custodita nel portachiavi del sistema) e
usa solo SQLite. Variabili facoltative:

| Variabile | Predefinito | Descrizione |
|---|---|---|
| `PORTO_UPDATE_CHECK` | `true` | Controllo giornaliero di una nuova versione; `false` = nessuna richiesta |
| `PORTO_DESKTOP_PORT` | ricordata, poi scelta dal sistema | Porta locale della finestra dell'app |
| `PORTO_DESKTOP_LAN_PORT` | `8443` | Porta HTTPS della rete locale |
| `PORTO_DESKTOP_KEYRING` | `auto` | `auto` (portachiavi del sistema, poi file) o `file` (`<dati>/secret.key`) |

Vedi anche [App desktop](/gestore/app-desktop/).
