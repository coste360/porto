## 26. Utenti, ruoli e autenticazione

### 26.1 Ruoli

Ruoli predefiniti per marina (un utente può avere ruoli diversi in marine diverse). I permessi sono costanti nel
codice e verificati nel servizio di dominio, come in Arenile (spec Arenile 16).

| Permesso | owner | director | office | accounting | dock | yard_mgr | yard | rental | viewer |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| Configurazione del marina, mappa, listini, modelli | ✔ | ✔ | | | | | | | |
| Utenti, ruoli, credenziali dei provider | ✔ | | | | | | | | |
| Clienti e barche | ✔ | ✔ | ✔ | lettura | lettura | lettura | | lettura | |
| Preventivi, contratti, transiti, rinnovi | ✔ | ✔ | ✔ | | transiti al banco | | | | |
| Spostamenti, rilasci, blocchi | ✔ | ✔ | ✔ | | ✔ | | | | |
| Arrivi, partenze, presenze, letture | ✔ | ✔ | ✔ | | ✔ | ✔ (alaggio e varo) | | | |
| Deroga di compatibilità (`occupancy.override_fit`) | ✔ | ✔ | | | | | | | |
| Diritti d'uso, cessioni, oneri, condominio | ✔ | ✔ | ✔ | ✔ | | | | | |
| Addebiti, scadenzario, solleciti | ✔ | ✔ | ✔ | ✔ | | | | | |
| Esportazione per la fatturazione, riscontro delle fatture | ✔ | ✔ | | ✔ | | | | | |
| Incassi e cassa | ✔ | ✔ | ✔ | ✔ | contanti | | | ✔ | |
| Rimborsi e addebiti di rettifica | ✔ | ✔ | | ✔ | | | | | |
| Rimessaggio e ipotesi | ✔ | ✔ | ✔ | | lettura | ✔ | | | |
| Cantiere: preventivi, commesse, planner | ✔ | ✔ | lettura | lettura | | ✔ | | | |
| Cantiere: ore lavorate e attività proprie | ✔ | ✔ | | | | ✔ | ✔ | | |
| Noleggio | ✔ | ✔ | ✔ | | | | | ✔ | |
| Manutenzioni | ✔ | ✔ | ✔ | | ✔ | ✔ | segnalazione | segnalazione | |
| Report ed esportazioni | ✔ | ✔ | operativi | ✔ | | operativi e cantiere | | noleggio | ✔ |
| Vista mappa | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ | ✔ |

`dock` = ormeggiatore, `yard_mgr` = capo cantiere, `yard` = operaio, `rental` = addetto al noleggio. Ruoli
personalizzati (insiemi di permessi) dopo la prima versione.

### 26.2 Autenticazione

Identica ad Arenile (spec Arenile 16.2): Argon2id, sessioni `HttpOnly` con CSRF double-submit, TOTP (obbligatorio
per `owner` e `accounting` se attivato), reset con token monouso, limiti dei tentativi, `ext.AuthProvider` per SSO,
chiavi API con scope e prefisso visibile (`po_live_…`), **token per dispositivo** per l'edizione desktop remota
(spec Arenile 29.2), chiavi dei sensori (24.2). CLI `porto admin create`.
