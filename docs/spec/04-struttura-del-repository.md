## 4. Struttura del repository

```
porto/
├── cmd/
│   ├── porto/                  # serve, migrate, admin create, seed demo, backup, restore, version
│   └── porto-desktop/          # launcher dell'edizione desktop (spec 31.2)
├── internal/
│   ├── app/                    # composition root, registry delle estensioni
│   ├── config/
│   ├── geo/                    # Cm, Cm2, Point, Polygon, Rect ruotati, intersezioni (puro)
│   ├── qty/                    # quantità a virgola fissa (puro)
│   ├── clock/ money/ calendar/ reqctx/ crypto/        # copiati da Arenile (3.3)
│   ├── http/
│   │   ├── admin/ public/ device/ ingest/ webhooks/ middleware/ sse/ static/
│   │   └── api/                # generato da api/openapi.yaml
│   ├── domain/
│   │   ├── marina/             # organizzazioni, marine, impostazioni, stagioni, chiusure
│   │   ├── harbor/             # mappe, elementi, posti, aree dinamiche, risorse a terra, generatori
│   │   ├── layout/             # geometria pura: celle, ingombri, validazione, silhouette
│   │   ├── fit/                # compatibilità barca ↔ posto (puro)
│   │   ├── occupancy/          # allocazioni su DB, hold, rilasci, spostamenti, ricerca
│   │   ├── presence/           # arrivi, partenze, assenze
│   │   ├── customer/ vessel/   # anagrafiche, documenti, scadenze
│   │   ├── catalog/            # articoli e servizi, codifiche, classificazioni
│   │   ├── pricing/            # motore prezzi puro
│   │   ├── pricelist/          # persistenza listini, simulatore, rinnovo
│   │   ├── contract/           # preventivi, contratti, transiti, charter, rate, rinnovi
│   │   ├── rights/             # diritti d'uso, cessioni, mandati, oneri, spese condominiali
│   │   ├── docs/               # modelli di stampa, renderer PDF, firma (porta SignatureProvider)
│   │   ├── charge/             # addebiti, scadenzario, solleciti, esportazione (porta ChargeExporter)
│   │   ├── payment/            # pagamenti, gateway online, crediti, riconciliazione col provider
│   │   ├── cash/ fiscal/       # sessioni di cassa, corrispettivi, FiscalDriver (come Arenile)
│   │   ├── accrual/            # competenze, ratei e risconti (puro)
│   │   ├── storage/            # rimessaggio, piani e ipotesi
│   │   ├── optimizer/          # disposizione nelle rimesse (puro)
│   │   ├── yard/               # cantiere: preventivi, commesse, attività, planner, consuntivi
│   │   ├── formula/            # linguaggio di formule dei prezzi di cantiere (puro)
│   │   ├── rental/             # noleggio: flotta, prenotazioni, consegna e restituzione
│   │   ├── maintenance/        # piani di manutenzione, interventi, blocchi
│   │   ├── sensor/             # dispositivi, letture, stati, anomalie
│   │   ├── profile/            # scheda pubblica del porto
│   │   ├── online/ checkout/   # pagina pubblica e flusso di prenotazione
│   │   ├── docai/              # estrazione documenti (porta DocumentExtractor)
│   │   ├── report/             # report di sola lettura
│   │   ├── notify/ event/ webhook/ apikey/ auth/ team/   # da Arenile
│   ├── store/                  # repository Bun, unico posto con SQL
│   ├── jobs/
│   └── migrations/{sqlite,postgres}/
├── pkg/ext/                    # interfacce pubbliche delle estensioni (spec 27.5)
├── api/openapi.yaml            # contratto API, fonte di verità
├── assets/harbor-kits/         # kit di illustrazioni della mappa (decorazioni, risorse a terra)
├── web/
│   ├── admin/                  # pannello
│   ├── booking/                # pagina pubblica di prenotazione (budget 180 KB gzip)
│   └── shared/                 # client generato, ui shadcn, mappa, planner, offline
├── deploy/  docs/  e2e/  scripts/agents/  .claude/  .devcontainer/  .github/
├── AGENTS.md  CLAUDE.md  Makefile  CHANGELOG.md
```

Comandi `make` identici ad Arenile (`setup`, `dev`, `seed`, `check`, `check-full`, `gen`, `fmt`, `test`,
`test-db`, `test-race`, `lint`, `e2e`).
