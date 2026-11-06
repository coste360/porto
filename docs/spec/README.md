# Porto — Specifica tecnica e funzionale

> **Nome:** *Porto* (logotipo "porto"; modulo Go `github.com/coste360/porto`, binario `porto`, prefisso delle
> variabili `PORTO_`). Marchio e design kit v1.0 in [`assets/brand/porto/`](../../assets/brand/porto/README.md),
> regole d'uso nell'[ADR 0007](../adr/0007-marchio.md). Resta la verifica di marchio e dominio (sezione 33).

Gestionale per porti turistici, marine, approdi e cantieri nautici sviluppato da Coste360 SRL. Copre l'intero
ciclo di un marina: configurazione della mappa del porto (banchine, pontili, posti barca, campi boe, aree a terra),
clienti e imbarcazioni, listini, contratti di ormeggio e transiti, cessioni e oneri, addebiti e incassi con
esportazione verso il software di fatturazione (la fattura resta fuori, come in Arenile), rimessaggio, cantiere, noleggio, prenotazione online dei posti barca, sensori di occupazione e report.

Riusa **la stessa infrastruttura di Arenile** (`../arenile`): un binario Go con SPA React incorporate, SQLite di
default o PostgreSQL, edizione desktop per Windows, macOS e Linux, app mobile per lo staff, API OpenAPI spec-first,
outbox degli eventi, job runner in-process. Dove una regola è identica ad Arenile la specifica lo dice e rimanda
alla sezione di Arenile invece di ripeterla; dove il dominio portuale richiede altro, la regola è scritta qui.

La specifica è **autorevole**: il codice segue la specifica, una deviazione richiede un ADR (`docs/adr/`).

---

## Indice

**Fondamenta**
1. [Obiettivi e principi](01-obiettivi-e-principi.md)
2. [Perimetro e tracciabilità dei requisiti](02-perimetro-e-tracciabilita.md)
3. [Architettura e stack](03-architettura-e-stack.md)
4. [Struttura del repository](04-struttura-del-repository.md)
5. [Modello di dominio e glossario](05-modello-di-dominio.md)
6. [Schema dati](06-schema-dati.md)

**Il porto**
7. [Mappa del porto: modello geometrico](07-mappa-modello-geometrico.md)
8. [Mappa: editor di configurazione](08-mappa-editor.md)
9. [Mappa: vista operativa, contesti e pontile dinamico](09-mappa-vista-operativa.md)
10. [Motore di occupazione](10-motore-di-occupazione.md)
11. [Clienti e imbarcazioni](11-clienti-e-imbarcazioni.md)

**Commerciale e amministrazione**
12. [Listini, articoli e motore prezzi](12-listini-articoli-prezzi.md)
13. [Contrattualistica](13-contrattualistica.md)
14. [Cessioni, oneri di gestione e spese condominiali](14-cessioni-oneri-condominio.md)
15. [Modelli di stampa e firma elettronica](15-modelli-di-stampa-e-firma.md)
16. [Addebiti, incassi ed esportazione per la fatturazione](16-addebiti-e-incassi.md)

**Operatività estesa**
17. [Rimessaggio e ottimizzazione delle rimesse](17-rimessaggio.md)
18. [Cantiere](18-cantiere.md)
19. [Noleggio](19-noleggio.md)
20. [Manutenzioni e risorse a terra](20-manutenzioni-e-risorse.md)

**Analisi, canali e servizi**
21. [Report e cruscotto statistico](21-report-e-cruscotto.md)
22. [Intelligenza artificiale: acquisizione documenti](22-intelligenza-artificiale.md)
23. [Prenotazione online dei posti barca](23-prenotazione-online.md)
24. [Sensori di occupazione in tempo reale](24-sensori-occupazione.md)
25. [Scheda pubblica del porto](25-scheda-pubblica.md)

**Piattaforma**
26. [Utenti, ruoli e autenticazione](26-utenti-ruoli.md)
27. [API, eventi, webhook ed estensioni](27-api-eventi-estensioni.md)
28. [Notifiche e comunicazioni](28-notifiche-e-comunicazioni.md)
29. [Tempo reale, offline e internazionalizzazione](29-tempo-reale-offline-i18n.md)
30. [Sicurezza, privacy e GDPR](30-sicurezza-privacy.md)
31. [Multipiattaforma: deploy, desktop e app mobile](31-multipiattaforma.md)
32. [Test, qualità e roadmap](32-test-qualita-roadmap.md)
33. [Decisioni aperte](33-decisioni-aperte.md)

Documenti collegati: [invarianti](../invariants.md), [ADR](../adr/README.md), [AGENTS.md](../../AGENTS.md).
