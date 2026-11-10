# Documentazione di Porto

Sito della documentazione costruito con [Astro Starlight](https://starlight.astro.build): italiano come lingua
sorgente (locale radice) e inglese, ricerca integrata (Pagefind), riferimento API generato da `api/openapi.yaml` a
ogni build, verifica dei link interni.

È un pacchetto npm a sé, fuori dai workspace di `web/`: `make setup` lo installa. Si usa tramite `make`:

| Comando | Cosa fa |
|---|---|
| `make docs-dev` | server di sviluppo con ricarica (http://localhost:4321) |
| `make docs-build` | build statica in `docs-site/dist`: ricerca, riferimento API, verifica dei link interni |
| `make docs-check` | verifica dei tipi (`astro check`) |
| `make docs-preview` | serve la build statica |
| `make docs-screenshots` | converte `docs/screenshots/*.png` in WebP in `src/assets/screenshots/` (`DOCS_ONLY=nome,nome`) |

## Struttura

- `src/content/docs/`: pagine in italiano (`gestore/`, `installazione/`, `api/`, `sviluppo/`), le stesse in `en/`
  con gli stessi percorsi. I link tra pagine sono assoluti dalla radice (`/gestore/…`, `/en/gestore/…`).
- `src/components/`: `<Screenshot name="…" dark="…" alt="…" />` (nome sconosciuto = build fallita) e `<Faq />`
  (domande con i dati strutturati `FAQPage`), importabili da `~/components`; `overrides/` contiene le sostituzioni dei
  componenti di Starlight (logo, "di Coste360.it", hero della home, piè di pagina, dati strutturati).
- `src/content/i18n/{it,en}.json`: le stringhe dei componenti (`porto.*`).
- `src/routeData.ts`: tiene le centinaia di link del riferimento API fuori dalla barra laterale delle altre pagine.
- `src/lib/base-links.mjs`: aggiunge il percorso base (`PORTO_DOCS_BASE`) ai link delle pagine.
- `scripts/sync-theme.mjs` (eseguito prima di `dev`, `build` e `check`): genera `src/styles/generated/theme.css`
  (ignorato da git) dai token di `web/shared/src/styles/globals.css` e verifica che i file del marchio in `public/`
  siano identici agli originali di `assets/brand/`.

## Marchio

Logo Porto (`public/brand/porto/`, da `assets/brand/porto/outlined/`), favicon e immagine social (da
`assets/brand/porto/06_Favicon_Web/` e `10_Materiale_Social/`) e logo Coste360 (`public/brand/coste360/`, da
`assets/brand/coste360/`) sono copie identiche dei file del kit, verificate a ogni build: © Coste360 SRL, fuori dalla
licenza del codice (`docs/adr/0007-marchio.md`). Non si modificano né si ridisegnano.

## Indirizzo e pubblicazione

Variabili d'ambiente facoltative: `PORTO_DOCS_SITE` (origine canonica, per canonical, sitemap, `robots.txt` e
immagine social; senza, nessuna sitemap), `PORTO_DOCS_BASE` (percorso base, per esempio `/porto/`),
`PORTO_DOCS_VERSION` (versione mostrata nel piè di pagina).

`.github/workflows/docs.yml` costruisce il sito sulle pull request, su `main` e sui tag di rilascio. Il deploy su
GitHub Pages si attiva con la variabile di repository `DOCS_PAGES=true`; `DOCS_SITE_URL` e `DOCS_BASE_PATH` fissano
origine e percorso (senza `DOCS_SITE_URL` si usa l'indirizzo Pages del repository). Il dominio non è ancora deciso.
