## 22. Intelligenza artificiale: acquisizione documenti

### 22.1 Obiettivo

Ridurre la trascrizione manuale dei documenti più frequenti: documento d'identità e patente del cliente, patente
nautica, licenza di navigazione o certificato d'iscrizione della barca, polizza assicurativa, visura camerale.
**L'operatore resta in controllo:** l'IA propone i campi, una persona li conferma; nessun dato estratto viene salvato
senza conferma.

### 22.2 Flusso

1. L'operatore (pannello, anche dal telefono con la fotocamera, o desktop) carica foto o PDF del documento e ne indica il tipo
   (o lascia che il sistema lo riconosca).
2. Il dominio `docai` invia l'immagine al driver configurato e riceve una struttura tipizzata per tipo di documento
   (schema JSON versionato in `internal/domain/docai/schemas`), con un livello di confidenza per campo.
3. Il pannello mostra il documento accanto al modulo precompilato; i campi a bassa confidenza sono evidenziati; i
   valori che differiscono da quelli già in anagrafica sono mostrati come proposta di modifica.
4. Alla conferma i dati vanno nell'anagrafica del cliente o della barca e nel documento (11.3), con l'utente che ha
   confermato e il driver usato (`extracted_by`, `confirmed_by`).
5. Validazioni deterministiche dopo l'estrazione: codice fiscale (carattere di controllo), date plausibili, scadenze
   nel passato evidenziate, dimensioni della barca in un intervallo realistico.

### 22.3 Interfaccia e driver

```go
type DocumentExtractor interface {
    Code() string
    Supports(docType DocumentType) bool
    Extract(ctx context.Context, req ExtractRequest) (*ExtractResult, error) // immagini, tipo, lingua
}
```

- **`none`** (default): funzione spenta, nessun dato esce dall'istanza.
- **`anthropic`** (core, facoltativo): modelli Claude con input di immagini e PDF e output strutturato secondo lo
  schema del tipo di documento. Chiave API del gestore cifrata nelle impostazioni; modello configurabile
  (`docai.model`, predefinito il modello Sonnet più recente disponibile al rilascio). Le immagini sono inviate solo
  per l'estrazione e non sono usate per altro dal sistema.
- Altri driver (modelli in locale, OCR di terze parti) come estensioni.

### 22.4 Privacy e sicurezza

- Attivazione esplicita del titolare con la presa visione di un'informativa: i documenti d'identità sono inviati a
  un fornitore esterno (responsabile del trattamento); la pagina delle impostazioni indica fornitore, finalità e
  conservazione.
- Nessun invio per i documenti caricati dalla pagina pubblica senza il consenso del cliente nel flusso.
- Limiti di frequenza per utente, dimensione massima dei file, registro delle estrazioni senza il contenuto dei
  documenti; mai dati estratti nei log.
- Non disponibile nell'edizione desktop senza internet (il pannello lo spiega).
