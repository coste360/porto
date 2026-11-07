# ADR 0007 — Marchio: Porto (design kit v1.0)

**Stato:** proposta · **Spec:** README, 31

## Contesto

Coste360 SRL ha fornito il design kit **Porto v1.0** (`assets/brand/porto/`). Contiene: simbolo (una "P" che
diventa banchina, con il pontile in negativo e lo specchio acqueo in azzurro), logotipo "porto" in minuscolo, icone
app, favicon, manifest web, immagini social, token CSS/JSON e linee guida PDF. Colori: navy `#0b2542`, sky
`#38bdf8`, nero `#111827`, bianco. Carattere del prodotto: Titillium Web. Sono la stessa palette e lo stesso
carattere di Arenile (ADR Arenile 0018) e del design system Coste360.

## Decisione

1. **Nome (D1, deciso dall'utente il 2026-11-07):** il prodotto si chiama "Porto", scritto "porto" nel logotipo e
   "Porto" nel testo, ed è **un prodotto Coste360**, accreditato come in Arenile: il pannello mostra la firma
   "un prodotto di" + logo Coste360 (§3).
2. **File:** il kit sta in `assets/brand/porto/` con la struttura originale e un `NOTICE.md`: tutti i diritti
   riservati a Coste360 SRL, esclusi dalla licenza del codice. Le copie usate dalle app (`web/shared/src/brand/`,
   icone del desktop) sono verificate identiche agli originali da un test, come in Arenile.
3. **Uso nell'interfaccia** (stesse regole di Arenile):
   - il marchio passa solo da un componente condiviso `BrandLogo` (`web/shared/src/ui/patterns/Brand.tsx`);
   - nessuna app carica Titillium Web solo per il logo: i loghi standard sono in tracciati;
   - il tema shadcn usa la palette Coste360 già adottata da Arenile: primario navy `#0b2542`, azzurro `#38bdf8`
     solo come dettaglio grafico (non raggiunge 3:1 sul bianco), focus e link nel blu più scuro verificato da test di
     contrasto;
   - la firma del produttore passa solo dal componente condiviso `MadeByCoste360`
     (`web/shared/src/ui/patterns/MadeByCoste360.tsx`): testo "un prodotto di" / "a product by" (i18n) e logo
     Coste360 ufficiale (`logo-primary.svg` su fondo chiaro, `logo-white.svg` su fondo scuro, copiati invariati da
     `assets/brand/coste360/` in `web/shared/src/brand/` e verificati identici dallo stesso test delle copie),
     link a https://coste360.it in una nuova scheda (`rel="noopener noreferrer"`). Sta nel piede della barra
     laterale del pannello (nascosta quando la barra è ridotta a icone), sotto la scheda di accesso e nel piede
     delle pagine fuori dal marina;
   - logo orizzontale su fondi chiari; `horizontal_inverted_2color` o `horizontal_on_navy` sui fondi scuri; area di
     rispetto di 1/4 della larghezza del simbolo; dimensione minima 24 px per il simbolo e 120 px per il logo
     orizzontale; niente ombre né gradienti.
4. **Dove si usa:**
   - pannello e pagina pubblica: favicon, `site.webmanifest`, icona PWA mascherabile;
   - edizione desktop: icone `porto_app_*` per `.exe`, `.app` e AppImage, simbolo monocromatico per la barra di
     sistema;
   - email: firma "Porto — Coste360" con il simbolo come immagine inline;
   - pagina pubblica: solo "powered by Porto", perché il marchio del porto resta in primo piano (spec 25).

## Conseguenze

- **Carattere del logotipo (D13, deciso dall'utente il 2026-11-07):** il wordmark "porto" è in **Titillium Web
  700**, lo stesso carattere dell'interfaccia e di Arenile, non in Inter Bold. Gli SVG standard del kit
  (`01_Loghi_SVG/`) hanno il wordmark in tracciati da Inter Bold (lo dice `08_Developer_Tokens/porto-usage.md`) e
  restano come originali del kit. `scripts/brand/build-brand.mjs` (`make brand`, progetto Node a sé con
  opentype.js, MIT, e il font di `@fontsource/titillium-web`, OFL-1.1) converte in tracciati il testo di
  `09_Sorgenti_Modificabili/*_editable.svg` con la stessa posizione, dimensione, spaziatura e allineamento, e
  scrive in `assets/brand/porto/outlined/` le stesse varianti orizzontali e verticali del kit (stessi nomi file,
  intestazione, sfondo, simbolo e colori del file standard corrispondente; cambia solo il wordmark). L'output è
  deterministico e versionato: build e CI non eseguono lo script. Le copie in `web/shared/src/brand/` dei loghi
  orizzontali vengono da `outlined/` e il test delle copie le confronta con quei file; simbolo e icone restano
  quelli del kit. Nessuna app carica Titillium Web per il logo.
- **Dimensione del wordmark (approvata dall'utente il 2026-11-07):** con la dimensione dei sorgenti modificabili
  (137 px orizzontale, 118 px verticale) il wordmark in Titillium risultava più piccolo di quello in Inter. Lo
  script lo ingrandisce a **176 px** (orizzontale, spaziatura -5) e **150 px** (verticale, spaziatura -4), così
  l'altezza della "o" coincide con quella del wordmark Inter dei file standard; posizione, linea di base e
  allineamento restano quelli dei sorgenti. I sorgenti modificabili del kit non cambiano: le dimensioni stanno in
  `WORDMARK_SIZE` di `scripts/brand/build-brand.mjs`, che le confronta con l'altezza misurata dal wordmark Inter di
  `01_Loghi_SVG/` (scarto massimo 2%) e verifica che il wordmark resti dentro il `viewBox`, invariato. Un nuovo
  ritocco si fa lì e si rigenera con `make brand`.
- Il `site.webmanifest` del kit ha descrizione "Il gestionale portuale di Coste360" e percorsi assoluti dalla radice:
  nel binario i percorsi diventano quelli serviti da `internal/http/static`.
