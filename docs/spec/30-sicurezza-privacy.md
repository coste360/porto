## 30. Sicurezza, privacy e GDPR

Regole di Arenile (spec Arenile 22 e 23) valide integralmente: segreti cifrati con AES-GCM da `PORTO_SECRET_KEY`,
header di sicurezza, rate limit, client di uscita anti-SSRF per webhook e provider, nessun dato personale nei log e
negli eventi SSE, audit di ogni scrittura, esportazione e anonimizzazione dei clienti.

Aggiunte del dominio portuale:
- **Documenti d'identità e della barca** cifrati a riposo nella cartella dati (chiave derivata da `PORTO_SECRET_KEY`),
  serviti solo tramite URL firmati a breve scadenza a utenti con `customers.read`; conservazione configurabile per
  tipo (es. documenti dei transiti cancellati dopo 24 mesi dall'ultima partenza), job di pulizia.
- **Conservazione:** addebiti esportati, pagamenti e corrispettivi non si cancellano con l'anonimizzazione; il
  cliente anonimizzato mantiene negli addebiti esportati lo snapshot dei dati fiscali, che serve a ricostruire le
  fatture emesse all'esterno.
- **Ingestione dei sensori** isolata: le chiavi dei dispositivi possono solo scrivere letture del proprio marina.
- **IA:** invio a fornitori esterni solo con attivazione esplicita e informativa (22.4).
- **Firma elettronica:** PDF firmati con hash registrato; evidenze OTP conservate con il documento.
- **Ruoli** con separazione tra operatività e amministrazione (26.1); TOTP consigliato per `accounting`.
