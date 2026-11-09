# Porto su CapRover

Installazione di Porto, il gestionale per porti turistici, approdi e cantieri nautici, su
[CapRover](https://caprover.com). L'immagine `ghcr.io/coste360/porto` è pubblica e multi-architettura
(`amd64`/`arm64`): nessuna compilazione sul server.

## Opzione 1: One-Click App (consigliata)

1. In CapRover: **Apps → One-Click Apps/Databases → TEMPLATE** (in fondo alla lista).
2. Incolla il contenuto di `one-click-app.yml` e premi **Next**.
3. Compila le variabili:
   - **Chiave segreta**: genera il valore con `openssl rand -base64 32` e conservalo insieme ai backup (senza di
     essa i dati cifrati non sono recuperabili);
   - email, nome, organizzazione e password (almeno 12 caratteri) del titolare, creato al primo avvio;
   - SMTP e mittente email (facoltativi).
4. A installazione completata, dalla scheda dell'app:
   - abilita **HTTPS** e **Force HTTPS**;
   - se usi un dominio personalizzato (per esempio `gestionale.mio-marina.it`) aggiorna `PORTO_BASE_URL`;
   - dopo il primo accesso puoi togliere `PORTO_ADMIN_PASSWORD` dalle **Environment Variables**.

L'immagine è distroless e non ha una shell: per creare un utente a mano usa, via SSH sul server,
`docker exec -e PORTO_ADMIN_PASSWORD='...' $(docker ps -qf name=srv-captain--porto) /porto admin create --email titolare@mio-marina.it --name "Mario Rossi" --org "Marina Esempio SRL"`.

## Opzione 2: app con `captain-definition`

`captain-definition` usa l'immagine già pubblicata `ghcr.io/coste360/porto:latest`, senza build sul server.

```sh
cd deploy/caprover
caprover deploy -a porto   # oppure carica un tar con captain-definition dalla dashboard
```

Poi configura l'app dalla dashboard:

- **Container HTTP Port**: `8080`;
- **Persistent Directories**: percorso `/data` (database SQLite, file caricati, backup);
- **Environment Variables**: almeno `PORTO_BASE_URL` (l'indirizzo https dell'app), `PORTO_DATA_DIR=/data` e
  `PORTO_SECRET_KEY`, più `PORTO_TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16` e le variabili
  `PORTO_BOOTSTRAP_OWNER_*` / `PORTO_ADMIN_PASSWORD` per il primo titolare (elenco completo in
  [`.env.example`](../../.env.example)).

## Protezione del container

I template Coolify e demo avviano Porto con filesystem in sola lettura, `/tmp` in memoria, tutte le capability
rimosse e `no-new-privileges`. Il formato One-Click App di CapRover non permette di esprimere queste opzioni in
`one-click-app.yml`: senza altri passi vale solo l'utente non root dell'immagine. Dopo l'installazione (con entrambe
le opzioni) aggiungile dalla scheda dell'app, **App Configs → Service Update Override**:

```yaml
TaskTemplate:
  ContainerSpec:
    ReadOnly: true
    CapabilityDrop:
      - ALL
```

Porto scrive solo in `/data`. Per avere anche `/tmp` in memoria (usato per i caricamenti più grandi) serve una voce
`Mounts` nello stesso override; poiché sostituisce l'elenco dei mount del servizio, deve ripetere il mount di
`/data` esattamente come lo mostra `docker service inspect srv-captain--porto` (sezione
`Spec.TaskTemplate.ContainerSpec.Mounts`), più `{Type: tmpfs, Target: /tmp}`. Senza quella voce `/tmp` resta in
sola lettura e un caricamento molto grande può fallire. `no-new-privileges` non si imposta dall'override sulle
versioni di Docker Swarm che non lo supportano; con l'utente non root e nessuna capability il rischio residuo è
basso. Dopo il salvataggio verifica che l'app riparta e risponda su `/readyz`.

## Note

- Il TLS è gestito da CapRover (nginx e Let's Encrypt): non impostare `PORTO_TLS_AUTOCERT_DOMAINS`.
- Health check: l'immagine non ha `curl`; usa `GET /healthz` (vivo) e `GET /readyz` (database e migrazioni) da un
  monitor esterno.
- Backup: `docker exec $(docker ps -qf name=srv-captain--porto) /porto backup --out /data/backup.porto-backup`,
  poi copia il file fuori dal server insieme a `PORTO_SECRET_KEY`.
- Aggiornamento: cambia il tag dell'immagine (per esempio `1.2.0`) o rifai il deploy di `latest`; le migrazioni
  vengono eseguite all'avvio. Fai un backup prima di cambiare versione.
- Per PostgreSQL usa [Coolify](../coolify/README.md) o il compose [`deploy/docker-compose.postgres.yml`](../docker-compose.postgres.yml).
