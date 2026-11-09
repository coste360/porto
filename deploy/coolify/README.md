# Porto su Coolify

Installazione di Porto, il gestionale per porti turistici, approdi e cantieri nautici, su
[Coolify](https://coolify.io), la piattaforma self-hosted per il deploy su VPS. Due template Docker Compose:

| File | Database | Quando usarlo |
|---|---|---|
| `porto.yml` | SQLite (file `/data/porto.db` nel volume `porto-data`) | Un porto o un cantiere: la scelta consigliata |
| `porto-postgres.yml` | PostgreSQL 17 (password generata da Coolify) | Più marina nella stessa organizzazione o infrastruttura già basata su PostgreSQL |

Entrambi usano l'immagine pubblica `ghcr.io/coste360/porto` (multi-architettura `amd64`/`arm64`), con file system in
sola lettura, utente non root e nessuna capability.

## Installazione

1. In Coolify: **Projects → + New → Docker Compose Empty**.
2. Incolla il contenuto di `porto.yml` (oppure `porto-postgres.yml`).
3. In **Environment Variables** imposta `PORTO_SECRET_KEY` con il valore generato da

   ```sh
   openssl rand -base64 32
   ```

   e conservalo insieme ai backup: cifra le chiavi dei provider, i documenti dei clienti e gli altri segreti. Senza
   questa chiave i dati cifrati non sono recuperabili.
4. Facoltativo: `PORTO_SMTP_URL` (`smtp://utente:password@host:587`) e `PORTO_MAIL_FROM` (con un dominio reale)
   per inviti, conferme di prenotazione e promemoria. Senza SMTP le email non vengono inviate.
5. Assegna il dominio al servizio `porto` (Coolify valorizza `SERVICE_FQDN_PORTO_8080` e quindi `PORTO_BASE_URL`)
   e premi **Deploy**.
6. Crea il primo utente titolare. L'immagine è distroless e **non ha una shell**, quindi la scheda **Terminal** di
   Coolify non funziona: imposta invece, prima del primo deploy,

   ```sh
   PORTO_BOOTSTRAP_OWNER_EMAIL=titolare@mio-marina.it
   PORTO_BOOTSTRAP_OWNER_NAME=Mario Rossi
   PORTO_BOOTSTRAP_OWNER_ORG=Marina Esempio SRL
   PORTO_ADMIN_PASSWORD=<almeno 12 caratteri>
   ```

   Il titolare viene creato all'avvio solo se il database non ha ancora utenti; ai riavvii successivi le variabili
   sono ignorate e dopo il primo accesso puoi togliere `PORTO_ADMIN_PASSWORD`. In alternativa, da una shell SSH sul
   server:

   ```sh
   docker exec -e PORTO_ADMIN_PASSWORD='...' <container-porto> /porto admin create \
     --email titolare@mio-marina.it --name "Mario Rossi" --org "Marina Esempio SRL" \
     --marina "Marina Esempio" --slug marina-esempio
   ```

7. Accedi al pannello su `https://<dominio>/admin/`.

## Note

- Il certificato TLS è gestito dal proxy di Coolify: non impostare `PORTO_TLS_AUTOCERT_DOMAINS`.
- `PORTO_TRUSTED_PROXIES` è preimpostata sulle reti private Docker, così Porto legge l'IP reale dei clienti
  (serve ai limiti di frequenza della prenotazione online e dell'accesso).
- Health check: configuralo in Coolify su `GET /healthz`, porta `8080` (l'immagine non contiene `curl`).
  `/readyz` verifica anche database e migrazioni.
- Backup: con SQLite `docker exec <container-porto> /porto backup --out /data/backup.porto-backup`, poi copia il
  file fuori dal server; con PostgreSQL usa anche i backup pianificati di Coolify sul servizio `postgres`. Conserva
  `PORTO_SECRET_KEY` con i backup.
- Aggiornamento: imposta `PORTO_VERSION` (per esempio `1.2.0`, oppure `1.2` per ricevere le correzioni) e rifai il
  deploy; le migrazioni vengono eseguite all'avvio. Fai un backup prima di cambiare versione.
- Per una vetrina pubblica con dati dimostrativi che si azzerano ogni notte vedi [`../demo`](../demo/README.md).
