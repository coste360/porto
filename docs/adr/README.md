# Architecture Decision Records

Formato e regole come in Arenile (`../arenile/docs/adr/README.md`): un file per decisione, numerazione progressiva,
stato `proposta`, `accettata`, `sostituita da NNNN`. Cambiare una decisione richiede un nuovo ADR.

Le decisioni di Arenile sull'infrastruttura (ADR Arenile 0001 monobinario, 0002 SQLite/PostgreSQL, 0003 vincolo
unique, 0004 prezzi puri, 0005 OpenAPI spec-first, 0009 shadcn, 0012 kit della mappa, 0014 pagamenti online,
0021 ESC/POS, 0025–0027 desktop e mobile, 0029 conflitti da riprovare) valgono anche per Porto come pattern: ADR 0001 le adotta, senza copiarne il codice.

| # | Titolo | Stato |
|---|---|---|
| [0001](0001-riuso-infrastruttura-arenile.md) | Arenile come riferimento, stesso stack | accettata |
| [0002](0002-notte-come-unita-di-occupazione.md) | La notte come unità di occupazione | proposta |
| [0003](0003-aree-dinamiche-a-celle.md) | Aree dinamiche a celle con vincolo unique | proposta |
| [0004](0004-mappa-in-scala-svg.md) | Mappa SVG in scala reale con culling e livelli di dettaglio | proposta |
| [0005](0005-modelli-markdown-e-pdf-in-go.md) | Modelli Markdown e PDF generato in Go | proposta |
| [0006](0006-linguaggio-di-formule.md) | Linguaggio di formule interno per il cantiere | proposta |
| [0007](0007-marchio.md) | Marchio: Porto (design kit v1.0) | proposta |
| [0008](0008-nessuna-intelligenza-artificiale.md) | Nessuna intelligenza artificiale, Porto lavora offline | accettata |
| [0009](0009-scelte-m5.md) | Scelte di M5: credito derivato, pagamenti offline, esportazione riproducibile | proposta |
| [0010](0010-edizione-desktop-m6a.md) | Edizione desktop (M6a): server nel processo, rete locale con certificato proprio | proposta |
| [0011](0011-offline-telefono-remoto-m6b.md) | Offline, telefono e desktop remoto (M6b): coda con doppia deduplica, proxy remoto, bridge su loopback | proposta |
| [0012](0012-report-e-cruscotto-m7.md) | Report e cruscotto (M7): righe grezze e calcolo puro, riquadro uguale ai suoi record | proposta |
| [0013](0013-prenotazione-online-m8.md) | Prenotazione online (M8): quote come contatori nel DB, hold concreto con idempotenza di dominio, conferma anche da canale verificato | proposta |
| [0014](0014-rimessaggio-manutenzioni-m9.md) | Rimessaggio e manutenzioni (M9): ipotesi separate dallo stato reale, ottimizzatore puro con seme, applicazione tutto o niente, consumi addebitati una volta | proposta |
| [0015](0015-cantiere-m10.md) | Cantiere (M10): formule pure in virgola fissa, preventivo riproducibile dallo snapshot, commessa creata nell'accettazione, mezzi senza doppie prenotazioni per vincolo del DB, magazzino rinviato | proposta |
