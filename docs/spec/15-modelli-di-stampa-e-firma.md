## 15. Modelli di stampa e firma elettronica

### 15.1 Modelli di stampa personalizzati

- Modelli per: preventivo, contratto (per tipo e termine), condizioni generali e regolamento del porto, proposta di
  rinnovo, richiesta di pagamento, ricevuta non fiscale, sollecito, estratto conto, rendiconto
  dei titolari, ripartizione delle spese, ordine di lavoro e preventivo di cantiere, contratto di noleggio e verbale
  di consegna, lettera libera.
- Formato: **Markdown esteso** con campi di unione e blocchi (ADR 0005): `{{customer.name}}`,
  `{{vessel.loa | meters}}`, `{{#each installments}}…{{/each}}`, `{{#if contract.term == "annual"}}`, tabelle,
  intestazione e piè di pagina, interruzione di pagina, logo, campo firma (`{{signature "customer"}}`).
- Il linguaggio dei modelli è chiuso: niente codice, niente accesso a file o rete; i campi disponibili sono un elenco
  documentato per tipo di modello, con anteprima su dati demo e su un record reale.
- Più lingue per modello (il documento usa la lingua del cliente se disponibile); versioni del modello conservate:
  ogni documento generato registra la versione usata e l'hash del PDF.
- Il renderer PDF è Go puro (`go-pdf/fpdf` più il layout del sottoinsieme Markdown), così funziona anche
  nell'edizione desktop senza browser headless. Pagine A4, carattere incorporato, PDF/A-2b per i documenti firmati.

### 15.2 Archivio dei documenti generati

Ogni PDF generato è salvato (`generated_documents`) con hash SHA-256, modello e versione, record di origine, utente;
i documenti firmati non si rigenerano: una modifica crea una nuova versione del contratto.

### 15.3 Stampa

Stampa dal browser della pagina PDF; stampa diretta su stampanti di rete per le ricevute (driver ESC/POS di Arenile,
ADR Arenile 0021) dall'edizione desktop o tramite il ponte locale.

### 15.4 Firma elettronica

Interfaccia `ext.SignatureProvider`:

```go
type SignatureProvider interface {
    Code() string
    // Request avvia una richiesta di firma per uno o più firmatari su un PDF.
    Request(ctx context.Context, req SignatureRequest) (*SignatureEnvelope, error)
    Status(ctx context.Context, envelopeRef string) (*SignatureEnvelope, error)
    // Download restituisce il PDF firmato e l'eventuale prova (audit trail del provider).
    Download(ctx context.Context, envelopeRef string) (*SignedDocument, error)
    Cancel(ctx context.Context, envelopeRef string) error
    ParseWebhook(r *http.Request, secret []byte) (*SignatureEvent, error)
}
```

Driver:
- **`paper`** (default): stampa, firma autografa, caricamento della scansione e conferma manuale.
- **`otp`** (core): firma elettronica semplice con codice monouso inviato all'email (e al telefono tramite
  `Notifier`) del firmatario; il PDF riceve una pagina di evidenze (identità dichiarata, IP, istanti, hash). Il valore
  legale della firma semplice è valutato dal gestore con il suo consulente: la documentazione lo dice chiaramente.
- **`infocert`** (estensione): firma elettronica avanzata o qualificata tramite i servizi InfoCert, con le
  credenziali del gestore cifrate nelle impostazioni. Il driver si implementa sulle API e sul contratto di servizio
  di InfoCert; i dettagli tecnici sono da confermare con il fornitore (sezione 33).

Il ciclo è guidato dal dominio `docs`: invio, promemoria al firmatario, webhook o polling dello stato (job), PDF
firmato salvato nell'archivio, evento `contract.signed` che attiva il contratto (13.3). Firmatari multipli
(cotitolari, legale rappresentante e marina) in ordine o in parallelo.
