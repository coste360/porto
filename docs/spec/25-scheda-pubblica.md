## 25. Scheda pubblica del porto

Ogni marina mantiene nel pannello la propria **scheda tecnica** (`marina_profile`), pubblicata dalla sua istanza su
`/p/{slug}` insieme alla pagina di prenotazione (sezione 23), solo se l'istanza è raggiungibile da internet:
- posizione (coordinate WGS84 dell'imboccatura), indirizzo, contatti, canale VHF, orari dell'ufficio e
  dell'ormeggiatore;
- caratteristiche: numero di posti, posti per i transiti, lunghezza massima, fondali (all'imboccatura, in banchina,
  minimo e massimo), tipo di fondale, ridosso dai venti, avvertenze per l'accesso (testo e immagini), fanali;
- servizi: acqua, energia (potenze), carburante (benzina, gasolio, orari), Wi-Fi, servizi igienici, docce,
  lavanderia, aspirazione acque nere, raccolta rifiuti, travel lift (portata), gru, scivolo, cantiere, rimessaggio,
  noleggio, parcheggio, ristorazione, negozi, trasporti;
- foto, mappa semplificata (dalla mappa del porto, senza dati personali), listino dei transiti (matrice 12.5) se il
  gestore lo pubblica, occupazione aggregata (24.5) se attiva, pulsante di prenotazione;
- dati strutturati JSON-LD (`Marina`, `Place`) per i motori di ricerca; più lingue.

Il portolano della brochure di riferimento è un prodotto editoriale separato, non un modulo del gestionale: è fuori
perimetro. La scheda resta nell'istanza del gestore e non viene inviata a nessuna directory esterna.
