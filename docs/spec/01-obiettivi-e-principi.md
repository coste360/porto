## 1. Obiettivi e principi

**Obiettivi**
- Un unico sistema per porti turistici e cantieri nautici: posti barca, contratti, addebiti e incassi,
  rimessaggio, cantiere, noleggio, prenotazione online, report. La fattura si emette con il software di
  fatturazione del gestore, alimentato dalle esportazioni di Porto (sezione 16).
- La **mappa del porto** come strumento di lavoro principale: configurabile dal gestore (banchine, pontili, finger,
  campi boe, piazzali, capannoni, parcheggi, box) e consultabile in più *contesti* (contratti, presenze, pagamenti,
  scadenze, dimensioni, sensori).
- Posizionamento **dinamico** delle barche, in scala reale, su pontili, banchine, piazzali e capannoni.
- Prenotazione online dei transiti con pagamento anticipato e garanzia della disponibilità dell'ormeggio.
- Funzionare su ogni piattaforma con lo stesso core: server online, PC dell'ufficio senza internet, telefono
  dell'ormeggiatore in banchina.

**Principi di progetto** (ereditati da Arenile, spec 1)
- **Un binario, un'immagine, un volume.** Nessun servizio esterno obbligatorio. Le integrazioni (pagamenti
  online, firma, sensori, canali, esportazione verso la fatturazione) sono driver opzionali.
- **Configurazione come dati.** Le differenze tra porti si esprimono con mappe, categorie, listini, modelli e
  policy, mai con condizioni nel codice.
- **Scala reale.** Ogni geometria è in centimetri interi; un'imbarcazione sulla mappa occupa esattamente il suo
  ingombro. Lo spazio è una risorsa misurata, non un'etichetta.
- **Correttezza prima di tutto sull'occupazione.** Due barche nello stesso spazio nella stessa notte sono un
  difetto bloccante, garantito dai vincoli del database (sezione 10).
- **L'operatore conferma.** Automatismi e ottimizzatori propongono; dati anagrafici, spostamenti massivi e documenti fiscali
  sono confermati da una persona.
- **API-first.** Pannello, pagina pubblica e desktop usano esclusivamente le API documentate.
- **Dati del gestore.** Esportazione completa sempre disponibile; nessun dato verso Coste360 senza una scelta
  esplicita.
