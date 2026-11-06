## 17. Rimessaggio e ottimizzazione delle rimesse

### 17.1 Contratti di rimessaggio

- `contracts.kind = storage`: barca a terra su piazzale o in capannone per un periodo (invernale, annuale, a mesi),
  con invaso (proprio o del marina, risorsa `cradle`), servizi collegati (alaggio, varo, lavaggio, invernaggio del
  motore) come articoli o come commesse di cantiere.
- Prezzo con le basi del listino (al m², a fascia, a mese), per tipo di area (coperta, scoperta).
- Collocazione su un'area `surface` (7.4) con posizione e rotazione; l'occupazione segue la sezione 10.

### 17.2 Planner della rimessa

Vista a due assi della stessa area: **pianta** (disposizione delle barche in scala alla data selezionata) e
**linea del tempo** (barra per barca con entrata e uscita previste). Scorrendo la data la pianta mostra arrivi e
uscite; le celle si colorano per occupazione; il contesto *Accessibilità* (17.4) evidenzia le barche bloccate.
Sulla pianta valgono la sessione di disposizione con Salva / Ripristina e il riquadro "Da collocare" (9.4); una
**preimpostazione** ("Preset") applica alla bozza la proposta dell'ottimizzatore (17.5) senza salvarla.

### 17.3 Ipotesi di spostamento

- Un'**ipotesi** (`storage_scenarios`) è una copia di lavoro delle collocazioni di un'area per un intervallo di
  date, modificabile con il drag & drop senza toccare le occupazioni reali. Più ipotesi per area, con nome e autore.
- L'ipotesi registra le mosse (`scenario_moves`: barca, posizione di partenza e di arrivo, data) e calcola gli
  indicatori: superficie usata, barche bloccate per data, numero di movimentazioni necessarie, barche ancora da
  collocare.
- Confronto affiancato di due ipotesi o di un'ipotesi con lo stato reale.

### 17.4 Accessibilità (uscita delle barche)

Una barca in un capannone pieno può essere fisicamente intrappolata. Funzione pura
`optimizer.Accessibility(area, placements, date) → per barca: {accessible, blocking_vessels[]}`:
- la griglia di celle libere (più le celle della barca stessa) deve contenere un percorso dal suo ingombro a un
  varco (`exits`) largo almeno `beam + margin` (ricerca in ampiezza sulla griglia con la barca trattata come
  rettangolo che trasla);
- se non esiste, le barche *bloccanti* sono quelle del taglio minimo trovato rimuovendo una barca alla volta in ordine
  di distanza dal varco (euristica, documentata come tale).

Un'uscita prevista (fine contratto, varo programmato, commessa) di una barca non accessibile in quella data è un
**avviso** nel planner e nell'elenco dei movimenti.

### 17.5 Ottimizzazione automatica

`optimizer.Arrange(area, vessels, period, constraints) → Scenario` (puro, deterministico con un seme):
- vincoli: celle bloccate, varchi, dimensioni, peso, invasi, barche fissate dall'operatore (`pinned`);
- obiettivo, in ordine: tutte le barche collocate; nessuna uscita prevista bloccata (le barche che escono prima
  vicino ai varchi); minimo numero di movimentazioni rispetto allo stato attuale; superficie libera contigua
  massima;
- algoritmo: ordinamento per data di uscita, collocazione *bottom-left fill* sulle celle con rotazioni a 90°, poi
  ricerca locale con scambi per un tempo massimo (default 3 s). È un'euristica: il risultato è una proposta, mai
  un'applicazione automatica.

**Applicare un'ipotesi:** `POST /storage/scenarios/{id}/apply` esegue tutte le mosse in una transazione con la
primitiva di spostamento (10.5) rivalidando ogni posizione sullo stato attuale; un solo conflitto annulla tutto e
riporta l'elenco. Le mosse applicate generano la lista di lavoro dei movimenti per il piazzale (ordine consigliato,
mezzi necessari), consultabile dal telefono.
