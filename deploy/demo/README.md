# Istanza dimostrativa pubblica di Porto

Ricetta per una demo pubblica di Porto (per esempio `demo.porto.example`) con il porto dimostrativo di
`porto seed demo`: un utente per ogni ruolo, mappa, contratti, transiti, cantiere, noleggio e sensori simulati. Ogni
notte i dati vengono **azzerati e ricaricati**, così i visitatori possono provare tutto senza conseguenze.

> [!WARNING]
> Non usare questa ricetta per un porto reale: la password della demo è pubblica e ogni modifica viene persa al
> reset. Nessuna email viene inviata (finiscono come file `.eml` nel volume) e nessun pagamento è configurato.

## File

| File | Cosa fa |
|---|---|
| `docker-compose.yml` | Il servizio `porto` (immagine `ghcr.io/coste360/porto`), volume `porto-demo-data`, porta solo su `127.0.0.1`, SMTP disattivato |
| `demo.env.example` | Variabili da copiare in `deploy/demo/.env`: indirizzo pubblico, chiave segreta, password pubblica della demo |
| `reset.sh` | Reset notturno: ferma il server, esegue `/porto seed demo --reset`, lo riavvia e aspetta `/readyz` |

## Installazione

Su un server con Docker e un reverse proxy TLS (Caddy, nginx, Traefik) che inoltra il dominio della demo a
`127.0.0.1:8080`:

```sh
git clone https://github.com/coste360/porto.git && cd porto
cp deploy/demo/demo.env.example deploy/demo/.env
# imposta PORTO_BASE_URL, PORTO_SECRET_KEY (openssl rand -base64 32) e PORTO_ADMIN_PASSWORD (pubblica)
docker compose -f deploy/demo/docker-compose.yml up -d
deploy/demo/reset.sh            # primo caricamento del porto dimostrativo
```

Gli utenti dimostrativi e il marina sono quelli descritti da `porto seed demo`; la password di tutti è
`PORTO_ADMIN_PASSWORD`, da pubblicare sulla pagina che presenta la demo.

## Reset notturno

`reset.sh` usa il comando `/porto seed demo --reset` con `PORTO_DEMO_INSTANCE=true` (solo nel container del reset),
che cancella tutti i dati dell'istanza (database, file caricati, email) e ricarica il porto dimostrativo con date
relative al giorno del reset. Il comando rifiuta di partire se il database contiene altro oltre alla demo
(dettagli in `docs/demo.md`). Il server viene fermato durante il
reset (circa un minuto). Se il reset fallisce lo script riavvia comunque il server ed esce con errore: se il comando
ha rifiutato il database i dati precedenti restano intatti; se invece l'errore è arrivato dopo il controllo (durante
la cancellazione o il caricamento) i dati possono essere già stati cancellati e la demo risultare vuota o parziale.
Il reset successivo, o uno lanciato a mano, ricarica la demo.

Esempio di `crontab` dell'utente che gestisce Docker sul server (alle 04:00, ora del server), con aggiornamento
all'ultima versione stabile dell'immagine:

```cron
0 4 * * * cd /srv/porto && PULL=1 deploy/demo/reset.sh >> /var/log/porto-demo-reset.log 2>&1
```

L'immagine è distroless e non ha una shell, quindi le *Scheduled Tasks* di Coolify o simili, che eseguono un comando
di shell dentro il container, non funzionano: il reset va pianificato sul server (cron o timer systemd), come sopra.
Variabili utili: `DEMO_RESET_CMD` per un comando di reset diverso, `COMPOSE_FILE_DEMO` per un altro file compose.

## Note

- La demo non deve contenere dati reali: chi la usa può inserire nomi ed email, che restano al massimo fino al
  reset successivo. Indica sulla pagina della demo che i dati sono pubblici e cancellati ogni notte.
- Limiti di frequenza: lascia attivi quelli predefiniti; `PORTO_TRUSTED_PROXIES` deve indicare il reverse proxy,
  altrimenti tutti i visitatori sembrano lo stesso indirizzo.
- Aggiornamento: `PULL=1` nel reset segue `PORTO_VERSION` (predefinito `latest`, cioè l'ultima versione stabile).
