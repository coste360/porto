# ADR 0008 — Nessuna intelligenza artificiale

- **Stato:** accettata
- **Data:** 2026-11-06
- **Sezioni della specifica:** 01, 02, 11.3, 19.4, 22, 23, 27.5, 30, 31, 32 (M4)

## Contesto

La prima stesura della specifica prevedeva l'acquisizione dei documenti (identità, patente, licenza di navigazione,
polizza) con un driver di IA facoltativo e disattivato di default, verso un fornitore esterno. Porto deve lavorare
offline (edizione desktop senza internet, server in sede) e non dipendere da servizi esterni.

## Decisione

Porto non include funzioni di IA né l'estensione `DocumentExtractor`. I dati dei documenti li inserisce
l'operatore, con il file del documento allegato e validazioni deterministiche. La sezione 22 resta come segnaposto.

## Conseguenze

- Niente dominio `docai`, niente colonne `extracted`/`extracted_by` nei documenti, niente consensi per l'IA.
- M4 non comprende più l'acquisizione documenti con IA.
- Reintrodurla richiede un nuovo ADR.
