---
title: Compilare dai sorgenti
description: Compilare il binario porto e l'immagine Docker dal repository, e avviarlo senza Docker.
sidebar:
  order: 4
---

**Prerequisiti:** Go 1.27 o successivo, Node.js 22 o successivo con npm, `make` e `git`. In alternativa apri il
repository nel *devcontainer* incluso.

```bash
git clone https://github.com/coste360/porto.git && cd porto
make setup     # moduli Go, golangci-lint in ./bin, dipendenze web ed e2e
make build     # compila le due applicazioni web e bin/porto (statico, senza CGO)
bin/porto version
```

`make build` incorpora nel binario il pannello e la pagina pubblica: il file `bin/porto` è tutto ciò che serve per
eseguire Porto.

## Avvio senza Docker

```bash
export PORTO_BASE_URL=https://porto.mio-marina.it
export PORTO_SECRET_KEY="$(openssl rand -base64 32)"   # conservala insieme ai backup
export PORTO_DATA_DIR=/var/lib/porto
bin/porto serve
```

Il server ascolta su `:8080` (`PORTO_ADDR`) e applica le migrazioni all'avvio (`PORTO_AUTO_MIGRATE=true`). Le altre
variabili sono in [Configurazione](/installazione/configurazione/). Per tenerlo in esecuzione usa il gestore di
servizi del sistema (per esempio un'unità systemd con `Restart=on-failure` e le variabili in un `EnvironmentFile`
leggibile solo dall'utente del servizio).

## Immagine Docker

```bash
make docker      # immagine ghcr.io/coste360/porto:dev da deploy/Dockerfile
```

## Comandi del binario

```text
porto serve                     # avvia il server
porto migrate up|status [--check]
                                # applica o mostra le migrazioni (--check: esce con 1 se ce ne sono in sospeso)
porto admin create --email E --name N --org ORG [--marina NOME --slug SLUG --timezone TZ --locale it|en]
                                # crea il titolare (password da PORTO_ADMIN_PASSWORD, --password-stdin o richiesta)
porto seed demo [--password-stdin]
                                # carica il porto dimostrativo, con un utente per ruolo
porto backup --out FILE         # backup portabile, uguale per SQLite e PostgreSQL
porto restore --in FILE [--force]
                                # ripristino da backup (--force svuota prima il database)
porto version                   # versione del binario
```

Codici di uscita: `0` riuscito, `1` errore, `2` uso errato.

## App desktop

Il launcher dell'app desktop si compila con `make desktop-build` (apre il pannello nel browser) o
`make desktop-build DESKTOP_TAGS=webview` (finestra propria, richiede CGO e il toolchain del sistema). I pacchetti non
firmati si producono con `make desktop-package-macos`, `make desktop-package-windows` e
`make desktop-package-linux`. Dettagli in
[docs/desktop-build.md](https://github.com/coste360/porto/blob/main/docs/desktop-build.md).
