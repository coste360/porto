## 8. Mappa: editor di configurazione

L'editor (solo pannello, permesso `harbor.configure`) è lo strumento con cui il gestore disegna il proprio porto.
Obiettivo di accettazione: un porto di 6 pontili e 400 posti configurato in meno di 30 minuti partendo da
un'ortofoto (sezione 32.3).

### 8.1 Area di lavoro

- Tela SVG in scala con pan, zoom (rotella, pizzico), "adatta alla vista", righello e scala grafica, coordinate del
  cursore in metri.
- Livelli attivabili: sfondo, acqua/terra, strutture, posti, aree dinamiche, risorse a terra, colonnine,
  decorazioni, etichette, perimetro della concessione.
- **Snapping** configurabile: griglia (`grid_cm`, default 50), bordi delle strutture (un posto si aggancia al lato
  di un pontile con l'anchor sul bordo e la rotazione perpendicolare), vertici, allineamento ad altri posti.
- Selezione singola, multipla (riquadro, shift), per pontile, per gruppo; trascinamento, rotazione, allinea e
  distribuisci, duplica, specchia, sposta di valori numerici.
- Pannello proprietà contestuale per ogni tipo di elemento (sezione 7); modifica di più posti insieme (stessi
  valori per i campi comuni).
- Annulla e ripeti (stack di comandi lato client), salvataggio esplicito.

### 8.2 Calibrazione dello sfondo

1. Carica l'immagine (o importa il perimetro della concessione da un file GeoJSON).
2. Indica due punti e la loro distanza reale in metri: l'editor calcola la scala.
3. Ruota l'immagine finché il nord coincide; facoltativamente inserisci tre punti con coordinate geografiche per la
   georeferenziazione (7.1).

### 8.3 Generatori

Funzioni pure in `internal/domain/harbor/generator.go` (anteprima nel client, applicazione in una transazione):

- **Pontile:** origine sulla banchina, direzione, lunghezza, larghezza, tipo; posti su lato sinistro, destro o
  entrambi, numero di posti o "riempi la lunghezza", larghezza e lunghezza dei posti (anche a scalare: posti più
  grandi verso la testata), finger ogni N posti (0 = ormeggio a corpo morto), posti in testata, codice del pontile e
  numerazione (`1..N` continua, dispari a sinistra e pari a destra, oppure per lato con prefisso), prefisso
  (`B-`), gruppo di destinazione, categoria di prezzo derivata dalla larghezza.
- **Posti lungo una banchina:** su una polilinea esistente, con larghezza o numero di posti, tipo d'ormeggio,
  numerazione.
- **Area dinamica** su un lato di pontile o un tratto di banchina, con modalità e celle.
- **Campo boe:** poligono, raggio di rotazione, disposizione a quinconce o a griglia, numerazione.
- **Griglia di risorse a terra:** righe × colonne di posti auto o box con dimensioni, corsia, numerazione.
- **Piazzale o capannone:** poligono, passo delle celle, varchi, corsie.

### 8.4 Validazioni

Al salvataggio (backend, `layout.ValidateMap`, funzione pura):
- **Errori bloccanti:** codici duplicati nella mappa; due posti con ingombri che si sovrappongono (intersezione
  di rettangoli ruotati con tolleranza di 1 cm); boe con ingombro fuori mappa; area dinamica con celle fuori dal
  proprio supporto; posto con `max_loa_cm > length_cm + tolleranza del posto` senza conferma esplicita
  (`overhang_allowed`).
- **Avvisi:** posto fuori dall'acqua, posto non agganciato a una struttura, fondale non indicato, posto senza
  categoria di prezzo, boe con cerchi di rotazione sovrapposti, colonnina senza posti collegati.

### 8.5 Versioni e modifiche con occupazioni attive

- Documento mappa versionato: `PUT /maps/{id}` con `If-Match`; in caso di conflitto il pannello mostra la
  differenza (come Arenile, spec Arenile 11.2).
- Un posto o una risorsa con occupazioni future **non si elimina**: si disattiva (`active = false`) e il sistema
  elenca contratti e transiti da spostare.
- **Ridimensionare** un posto con occupazioni future richiede la verifica di compatibilità (`fit`) di ogni
  imbarcazione assegnata; le incompatibilità sono mostrate e vanno confermate o risolte prima del salvataggio.
- Modificare un'area dinamica (lunghezza, celle, margini) con occupazioni future è consentito solo se ogni
  posizionamento futuro resta valido; altrimenti il salvataggio è rifiutato con l'elenco dei conflitti.
- Rinumerazione di un pontile o di una selezione (anteprima, nessun effetto sui contratti: gli id non cambiano).
- Ogni salvataggio scrive audit ed evento `config.map_changed` (sezione 27.3).

### 8.6 Gruppi e categorie

- **Gruppi** (`berth_groups`): insiemi nominati di posti, aree e risorse, anche sovrapposti (un posto può stare in
  "Pontile B" e in "Transiti"). Usati da listini, quote online, report, filtri della mappa e comunicazioni.
- **Categorie di prezzo** (`price_class`) assegnate per posto o in blocco dalla selezione; il generatore le propone
  dalla larghezza (`L8`, `L10`, `L12`, ...), configurabili per marina.

### 8.7 Importazione ed esportazione

- Esportazione e importazione della configurazione della mappa in JSON versionato (backup, migrazione tra
  istanze) e in GeoJSON se georeferenziata.
- Importazione dell'elenco dei posti da CSV (codice, pontile, lunghezza, larghezza, fondale, categoria) con
  generazione della geometria lungo i pontili esistenti: utile per chi arriva da un altro gestionale.
- Stampa della mappa in A4/A3 in scala con legenda (pagina stampabile, come `internal/printout` di Arenile).
