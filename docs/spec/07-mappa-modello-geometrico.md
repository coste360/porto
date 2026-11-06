## 7. Mappa del porto: modello geometrico

### 7.1 Mappe e sistema di coordinate

- Un marina ha **più mappe** (`maps.kind`): `water` (specchio acqueo con banchine, pontili, boe), `yard`
  (piazzale), `shed` (capannone o rimessa coperta), `parking`, `boxes`, `other`. Ogni mappa ha nome, ordine e
  icona; il pannello le mostra come schede.
- Coordinate **in centimetri interi** (`geo.Cm`, `int64`), origine in alto a sinistra, asse y verso il basso,
  angoli in decimi di grado (`rotation_dd`, intero 0–3599). La mappa ha `width_cm × height_cm` (massimo
  500.000 × 500.000, cioè 5 km). Il `viewBox` SVG coincide con le coordinate: la scala è reale.
- **Georeferenziazione facoltativa:** tre punti di controllo (cm ↔ WGS84) definiscono una trasformazione affine
  (`maps.georef`). Serve per sovrapporre l'ortofoto, il perimetro della concessione demaniale (importato da un
  file GeoJSON o disegnato a mano) e la posizione dei sensori, e per esportare la mappa in GeoJSON.
- **Sfondo:** immagine caricata (ortofoto, planimetria in PNG/JPEG/SVG), con scala e rotazione calibrabili
  indicando due punti di distanza nota, oppure il perimetro della concessione. Opacità regolabile.
- **Acqua e terra:** poligoni facoltativi `water_polygons` e `land_polygons` per il disegno; un posto fuori
  dall'acqua è un avviso dell'editor, non un errore.

### 7.2 Strutture

Elementi strutturali (non occupabili) in `harbor_structures`:

| Tipo | Geometria | Attributi |
|---|---|---|
| `quay` banchina | polilinea (punti in cm), lato d'ormeggio `left`/`right` rispetto al verso | nome, quota, fondale per tratto (`depth_profile`: lista `{from_cm, to_cm, depth_cm}`), passo delle bitte |
| `pier` pontile | asse (segmento o polilinea) + larghezza | nome, codice (es. `B`), `fixed`/`floating`, lati utilizzabili `left`/`right`/`head`, fondale, radice collegata a una banchina |
| `finger` | rettangolo ruotato agganciato a un lato di pontile | lunghezza, larghezza, portata; separa due posti |
| `breakwater` diga | poligono o polilinea con spessore | nome |
| `channel` canale / imboccatura | poligono | fondale minimo, nome |
| `walkway` passerella a terra | polilinea con larghezza | |

### 7.3 Posti barca

Ogni posto (`berths`) è un **rettangolo ruotato** definito da:

- `anchor`: punto medio del lato d'accosto (sul bordo del pontile, della banchina o del finger), `rotation_dd`
  (direzione verso il largo), `width_cm` (larghezza utile) e `length_cm` (lunghezza utile). L'ingombro è il
  rettangolo che parte dall'anchor verso il largo.
- `code` univoco nella mappa (es. `B-12`), `pier_id` o `quay_id` di riferimento, `side`, `position` lungo il
  pontile (per ordinamento e rinumerazione), `group_ids`.
- **Limiti:** `max_loa_cm` (lunghezza fuori tutto massima, di default `length_cm`), `max_beam_cm` (di default
  `width_cm − margine`), `depth_cm` (fondale), `max_air_draft_cm` (solo se c'è un ponte o un ostacolo),
  `max_displacement_kg` facoltativo.
- **Ormeggio** (`mooring_type`): `mooring_line` (corpo morto e trappa, ormeggio di poppa o di prua),
  `finger` (di fianco al finger), `alongside` (all'inglese lungo la banchina), `piles` (tra pali), `buoy` (boa).
- **Utenze:** `pedestal_id` (colonnina), `power_a` (16, 32, 63, 125 A), `power_phase` (`single`, `three`),
  `water`, `wifi`.
- `price_class` (categoria per i listini, di default dalla larghezza: es. `L12` per posti da 12 m),
  `attributes` (tag liberi: `transit_only`, `superyacht`, `accessible`, `near_services`), `active`,
  `online_sellable`.

**Boe:** un posto con `mooring_type = buoy` ha geometria circolare: `center`, `swing_radius_cm` (raggio di
rotazione, ricavato dalla lunghezza massima più la catena). Due boe si sovrappongono se i cerchi si intersecano:
l'editor lo segnala; la regola di occupazione resta per boa.

### 7.4 Aree dinamiche

Un'area dinamica (`dynamic_areas`) non ha posti predefiniti: le barche vi sono collocate in scala, dove c'è spazio.

**Lineare** (`kind = linear`) — il *pontile dinamico*:
- Agganciata a un lato di pontile o a un tratto di banchina: `structure_id`, `side`, `from_cm`, `to_cm` lungo
  l'asse (lunghezza utile `L`).
- `mooring_mode`: `stern_to` (barche di poppa, una accanto all'altra: lungo l'asse occupano la **larghezza**)
  oppure `alongside` (di fianco: occupano la **lunghezza**).
- `cell_cm` (default 50), `margin_cm` (spazio minimo tra barche per i parabordi, default 50 per `stern_to`,
  100 per `alongside`), `max_loa_cm` e `max_beam_cm` (profondità dell'area verso il largo per `stern_to`),
  `depth_profile`.
- Le celle sono numerate `0 … ⌈L / cell_cm⌉ − 1` lungo l'asse.

**Superficie** (`kind = surface`) — piazzale, capannone, rimessa:
- Poligono `outline` sulla mappa (`yard` o `shed`), `cell_cm` (default 100, griglia allineata a un angolo
  `grid_rotation_dd`), `margin_cm` (default 60).
- Celle marcate come **non utilizzabili** (`blocked_cells`: corsie, pilastri, scaffali) e **varchi**
  (`exits`: celle da cui si entra ed esce, per il calcolo dell'accessibilità, 17.4).
- `max_loa_cm`, `max_height_cm` (capannoni), `max_weight_kg` per cella facoltativo, `supports`: tipi di
  invaso ammessi (`cradle`, `trailer`, `rack`).

### 7.5 Ingombro di un'imbarcazione e celle occupate

Funzione pura `layout.Footprint(vessel, placement, area) → cells` (ADR 0003):

- **Lineare `stern_to`:** occupa lungo l'asse l'intervallo `[x, x + beam + margin)`; **lineare `alongside`:**
  `[x, x + loa + margin)`. `x` è la posizione della barca in cm (agganciata a 10 cm). Il margine si applica una sola
  volta, sul lato verso l'asse crescente, e `x` non può scendere sotto `margin_cm / 2` dal bordo iniziale.
- Le celle occupate sono **tutte quelle che l'intervallo tocca**, anche in parte: `⌊x / c⌋ … ⌈(x + w) / c⌉ − 1`.
  L'arrotondamento è sempre per eccesso, quindi due barche con celle disgiunte non possono sovrapporsi
  fisicamente (al prezzo di al massimo una cella persa per lato).
- **Superficie:** rettangolo `loa + margin` × `beam + margin` con rotazione multipla di 90° rispetto alla griglia
  (le rotazioni libere non sono ammesse sulle superfici nella prima versione); celle = tutte quelle intersecate.
  Una cella `blocked` nell'ingombro rende il posizionamento non valido.
- Un posizionamento fuori dall'area o con fondale insufficiente in una cella è rifiutato da `fit` (sezione 10.2).

### 7.6 Risorse a terra

`land_resources`: elementi discreti su qualsiasi mappa, con `kind` (`parking`, `box`, `storeroom`, `locker`,
`cradle` invaso, `trailer_spot`, `custom`), `code`, rettangolo ruotato, dimensioni, `price_class`, `attributes`,
`active`. Si occupano per notte come i posti (sezione 10), si vendono come articoli del listino e si collegano a un
contratto (es. posto auto incluso nel contratto annuale) o si contrattualizzano da sole.

### 7.7 Elementi decorativi e servizi

`map_decorations` con illustrazioni del kit `assets/harbor-kits` (stesso meccanismo del kit di Arenile, ADR
Arenile 0012: sprite `<symbol>`, manifest generato, id validati dal backend): capitaneria/ufficio, distributore di
carburante, servizi igienici, docce, lavanderia, travel lift, gru, scivolo, fanali d'ingresso (rosso, verde),
faro, colonnina antincendio, raccolta rifiuti e oli esausti, pompa di aspirazione acque nere, parcheggio, ingresso,
bar, ristorante, testo libero, quote batimetriche (`depth_label`), freccia del nord, scala grafica.

**Colonnine** (`pedestals`): punti con codice, prese (numero, amperaggio), contatori acqua e energia facoltativi;
ogni presa è collegata a uno o più posti. Servono alla mappa, alle letture dei consumi (20.4) e alle manutenzioni.

### 7.8 Silhouette delle imbarcazioni

Le barche sono disegnate **in scala**: la silhouette è un path SVG generato da una funzione pura
(`web/shared/src/map/vessel-shape.ts`, specchiata in Go per le stampe) a partire da `loa_cm`, `beam_cm` e
`vessel.hull_type` (`monohull_motor`, `monohull_sail`, `catamaran`, `trimaran`, `rib`, `gozzo`, `other`). Il colore
viene dal contesto attivo (9.2); il nome della barca compare quando è leggibile (≥ 9 px), altrimenti nel tooltip.
