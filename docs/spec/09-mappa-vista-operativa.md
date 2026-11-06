## 9. Mappa: vista operativa, contesti e pontile dinamico

### 9.1 Rendering e prestazioni

- SVG in coordinate reali (7.1), pan e zoom touch e rotella, un solo sprite del kit per pagina.
- **Prestazioni:** fino a 3.000 elementi occupabili restano fluidi (≥ 50 fps durante il pan) su un tablet di
  fascia media. Ci si arriva con culling per viewport, livelli di dettaglio (sotto una soglia di zoom le
  silhouette diventano rettangoli pieni e le etichette spariscono) e memoizzazione per elemento (ADR 0004).
- Accessibilità: ogni posto è un `<g role="button">` con `aria-label` (codice, stato, barca); alternativa a
  elenco per pontile sempre disponibile; lo stato non dipende dal solo colore (glifi, 9.2).
- Selettore della **data** (oggi, frecce, calendario, "Vedi in data") e dell'**intervallo** (per ricerche e
  transiti), tema chiaro e scuro, "Stampa mappa".
- **Due modi di disegno** della stessa mappa, scelti dall'utente e ricordati per utente:
  - **In scala**: geometria reale (7.3), silhouette delle barche in scala;
  - **Schema**: ogni posto è una **tessera** piena con il codice al centro (e il nome della barca se c'è spazio),
    colorata per intero dal contesto, come le postazioni del kit Moderno di Arenile. Le tessere mantengono
    posizione e ordine lungo il pontile ma hanno una dimensione minima leggibile (il codice resta ≥ 11 px alla vista
    d'insieme), quindi i pontili si allungano rispetto alla scala reale. È la vista più adatta al lavoro quotidiano
    al banco e non richiede un'ortofoto. Le aree dinamiche e le rimesse restano sempre in scala, perché lì la misura
    è l'informazione.
- Sotto la mappa (sotto, sul telefono) i riquadri **Arrivi e partenze di oggi** e **Ingressi e uscite di oggi**
  (9.5), sincronizzati con la data scelta.

### 9.2 Contesti (una mappa, molti punti di vista)

Il selettore **Contesto** cambia colori, glifi e legenda senza cambiare mappa. Ogni contesto è una funzione pura
`lens(element, day_view) → {color_token, glyph, badge, label}` in `web/shared/src/map/lenses/`, alimentata dalla
stessa *vista del giorno* del server (`GET /maps/{id}/day?date=`).

| Contesto | Colori (token del tema) | Glifi |
|---|---|---|
| **Occupazione** (predefinito) | libero, contratto annuale, stagionale, mensile, transito, opzione/hold, rilasciato, bloccato, manutenzione | stella annuale, valigia transito, orologio hold, freccia rilascio, chiave manutenzione |
| **Tipo di contratto** | ormeggio annuale / stagionale / periodo, transito, charter, diritto d'uso, mandato di gestione | iniziale del tipo |
| **Tipo di imbarcazione** | motore, vela, catamarano, gommone, gozzo, altro | silhouette |
| **Presenza** | in porto, fuori (rientro previsto), assente da più di N giorni, a terra, in cantiere, arrivo atteso oggi, partenza attesa oggi | freccia entrata/uscita |
| **Stato pagamenti** | in regola, rata in scadenza (≤ 15 giorni), scaduto, scaduto oltre 60 giorni, contenzioso | euro, punto esclamativo |
| **Scadenze** | contratto, assicurazione, documenti della barca o del cliente in scadenza o scaduti | calendario |
| **Adeguatezza** | rapporto ingombro barca / posto (verde ≥ 85 %, giallo 60–85 %, rosso < 60 %, viola oltre i limiti con deroga) | percentuale |
| **Diritti d'uso** | posto del marina, posto di un titolare, titolare con mandato di gestione, oneri insoluti | chiave |
| **Manutenzione** | regolare, intervento in scadenza, intervento scaduto, fuori servizio | chiave inglese |
| **Sensori** | libero, occupato, discordanza con l'occupazione attesa, sensore offline o batteria bassa | antenna |
| **Utenze** | potenza della presa, acqua, consumi del mese | fulmine |
| **Anomalie** | barca presente senza allocazione, allocazione senza barca arrivata oltre l'orario, barca fuori dai limiti del posto (deroga o misure cambiate), contratto attivo con assicurazione scaduta, discordanza con il sensore, barca con due presenze attive | punto esclamativo |

- I contesti si scelgono da **schede** sopra la mappa (Occupazione, Contratti, Barche, Presenze, Pagamenti, …);
  quelli meno usati stanno in un menu "Altri".
- **La legenda è il filtro:** ogni voce della legenda del contesto ha una casella e il **conteggio** degli elementi
  in quello stato alla data scelta (es. Anomalie 33, Annuale 96, Transito 3); togliere la spunta spegne quegli
  elementi; "Rimuovi tutti i filtri" e "Seleziona / deseleziona tutto". Cliccare il conteggio apre l'elenco degli
  elementi.
- Il contesto **Anomalie** è calcolato dal server nella vista del giorno (funzione pura di occupazioni, presenze,
  documenti e stati dei sensori), così il conteggio è lo stesso su pannello, desktop e app.
- **Filtri** combinabili con il contesto: gruppo, pontile, categoria, lunghezza della barca, tipo di contratto,
  cliente, testo libero (barca, cliente, codice, matricola). Gli elementi esclusi diventano grigi, non spariscono.
- I colori del contesto sono token del tema (`--harbor-*`) personalizzabili nel tema del pannello; contrasto ≥ 3:1
  sull'acqua nei due temi.
- I dati personali compaiono solo con il permesso `customers.read` (altrimenti il nome della barca e il codice).

### 9.3 Scheda rapida e azioni

Tocco su un posto, una barca o una risorsa: scheda con barca (nome, dimensioni, foto), cliente, contratto e
periodo, saldo e rate scadute, presenza, documenti in scadenza, letture del sensore, note. Azioni secondo i
permessi: nuovo transito, nuovo preventivo, registra arrivo / partenza, sposta, rilascia notti, blocca, apri
manutenzione, incassa, apri contratto, apri cliente.

### 9.4 Pontile dinamico: posizionamento con drag & drop

Ogni posizionamento (su posto fisso, area dinamica lineare, piazzale, capannone, risorsa) si cambia trascinando la
silhouette in scala.

**Sessione di disposizione (Salva / Ripristina).** Su un'area dinamica o una rimessa l'operatore apre la
disposizione dell'area alla data scelta e lavora in **bozza**: sposta più barche, le toglie, ne aggiunge, poi preme
**Salva** (tutte le mosse in una sola transazione, con la stessa primitiva delle ipotesi, 17.3 e 17.5; un conflitto
annulla tutto e indica la barca) oppure **Ripristina** (scarta la bozza). La bozza è dell'utente, non blocca gli
altri, e al salvataggio viene rivalidata sullo stato attuale. Sui posti fissi il trascinamento singolo resta
immediato.

**Riquadro "Da collocare".** Accanto all'area aperta, l'elenco delle barche che dovrebbero stare lì nella data e non
hanno ancora una posizione (contratti sull'area, transiti assegnati all'area, arrivi del giorno, alaggi previsti),
con nome, misure e sagoma. Si trascinano nell'area; "Togli dall'area" riporta una barca nell'elenco. Contatore delle
barche da collocare sempre visibile.

**Le misure non si stirano.** La sagoma ha le maniglie solo per spostare e ruotare (di 90° sulle superfici): la
dimensione viene dall'anagrafica della barca (misure misurate, 11.2). Per correggerla si apre la scheda della
barca.

- **Durante il trascinamento** il client calcola con la stessa funzione `Footprint` del server (7.5, specchiata in
  TypeScript e verificata da test di parità sugli stessi casi JSON) le celle che la barca occuperebbe e mostra:
  verde se libere e compatibili per tutto il periodo, giallo se compatibili con avvisi (fondale al limite,
  sbalzo), rosso se in conflitto, con la barca e le notti in conflitto.
- Sulle aree lineari la barca **scorre lungo l'asse** con snapping a 10 cm e aggancio al bordo della barca vicina
  più il margine; un indicatore mostra lo spazio libero a sinistra e a destra in metri.
- **Al rilascio** il pannello chiede *da quando* vale lo spostamento (oggi, da una data, solo per un intervallo) e
  mostra l'effetto sul prezzo se cambia categoria; la conferma chiama `POST /occupancies/move` (10.5). Il server
  ricontrolla tutto: l'anteprima del client non è mai una garanzia.
- Spostamenti tra mappe: dall'acqua al piazzale (alaggio) o viceversa (varo) generano anche, se configurato, una
  richiesta di servizio di travel lift (18.7).
- **Compattazione** di un'area lineare: proposta che avvicina le barche per liberare uno spazio contiguo
  (anteprima, conferma, un solo spostamento transazionale).

### 9.5 Arrivi, partenze e presenze

- Pannello laterale del giorno: arrivi attesi (transiti, rientri annunciati), partenze attese, barche fuori, barche
  arrivate senza prenotazione (da sensori o segnalazione).
- Registrazione di arrivo e partenza dalla scheda, dall'elenco o dal pannello sul telefono dell'ormeggiatore, con ora, ormeggiatore,
  note e foto facoltativa.
- **Arrivo non previsto:** l'ormeggiatore indica barca (anche nuova, dimensioni stimate) e posto: il sistema crea
  un transito in `held` sul posto (se libero) da completare in ufficio.

### 9.6 Ricerca delle disponibilità

Pannello laterale **"Imbarcazione in ingresso"**, sempre a portata accanto alla mappa: tipo (**Transito** o
**Contratto**), tipo di risorsa (posto barca, area dinamica, piazzale, capannone, posto auto, box), dimensioni della
barca (o barca esistente, o una fascia di dimensione), date di arrivo e partenza, preferenze (ormeggio, utenze,
gruppo) e l'opzione **"Considera liberi i posti con la barca fuori"**: include i posti occupati da contratto con la
barca fisicamente assente per tutto il periodo (presenza `out` o sensore libero). Una soluzione di questo tipo è
marcata e, se scelta, crea anche la proposta di rilascio delle notti (10.6), che segue la `release_policy` del
contratto; senza rilascio confermato il posto non si vende. Il server risponde con l'elenco ordinato delle soluzioni (10.7): posti fissi liberi e
compatibili, spazi liberi nelle aree dinamiche con la posizione proposta, eventuali soluzioni con un cambio di
posto. La mappa evidenzia le soluzioni con il loro prezzo; un tocco crea il preventivo o il transito. Una ricerca
senza soluzioni può essere salvata come **richiesta non evasa** (13.2), per misurare la domanda persa.

### 9.7 Calendario degli ormeggi

Vista a griglia **posti × giorni** per pontile, gruppo o area (lo stesso componente planner di rimessaggio e
noleggio): una barra per allocazione colorata dal contesto, con barca e cliente; rilasci e blocchi visibili; spazi
liberi evidenziati per la dimensione cercata. Trascinare una barra in orizzontale cambia le date (allungamento o
riduzione, 13.5), in verticale cambia posto (10.5); creare una barra su uno spazio libero apre il transito. Per le
aree dinamiche la riga è l'area e le barre mostrano i metri occupati. Utile soprattutto per i transiti estivi.
