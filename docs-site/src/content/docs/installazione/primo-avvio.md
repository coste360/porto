---
title: Primo avvio
description: Creare il titolare, l'organizzazione e il primo marina, poi configurare il porto.
sidebar:
  order: 11
---

## 1. Il titolare

Il primo utente è il **titolare** (ruolo `owner`) di un'**organizzazione**, che può avere uno o più marine. Si crea in
uno di tre modi:

- **all'avvio**, con `PORTO_BOOTSTRAP_OWNER_EMAIL`, `PORTO_BOOTSTRAP_OWNER_NAME`, `PORTO_BOOTSTRAP_OWNER_ORG` e
  `PORTO_ADMIN_PASSWORD`: il titolare viene creato solo se il database non ha ancora utenti. Dopo il primo accesso
  togli `PORTO_ADMIN_PASSWORD` dalla configurazione;
- **da riga di comando**, anche insieme al primo marina:

  ```bash
  PORTO_ADMIN_PASSWORD='scegli-una-password-lunga' porto admin create \
    --email titolare@mio-marina.it --name "Mario Rossi" --org "Marina Esempio SRL" \
    --marina "Marina Esempio" --slug marina-esempio --timezone Europe/Rome --locale it
  ```

  Con Docker: `docker compose -f deploy/docker-compose.yml exec -e PORTO_ADMIN_PASSWORD='…' porto /porto admin create …`;
- **nell'app desktop**, dalla pagina del primo avvio che si apre da sola.

La password deve avere almeno 12 caratteri.

## 2. Il marina

Accedi al pannello su `https://<tuo-dominio>/admin/`. Se non hai creato il marina dalla riga di comando, crealo dalla
pagina dell'organizzazione: nome, slug (usato negli indirizzi pubblici `/b/{slug}` e `/p/{slug}`), fuso orario e
lingua. Le date di Porto sono sempre nel fuso orario del marina.

## 3. Configurare il porto

1. **Mappa**: disegna banchine, pontili e posti con i generatori, oppure importa i posti da CSV
   ([Mappa del porto](/gestore/mappa-del-porto/)).
2. **Listini**: articoli, categorie dei posti, periodi e prezzi ([Listini e prezzi](/gestore/listini-e-prezzi/)).
3. **Team**: invita i colleghi con il ruolo giusto ([Team e sicurezza](/gestore/team-e-sicurezza/)).
4. **Contratti e modelli di stampa** ([Contratti e transiti](/gestore/contratti-e-transiti/)).
5. Facoltativi: email (SMTP), pagamenti online, prenotazione online e scheda pubblica.

Per provare prima su dati realistici carica il porto dimostrativo in un'installazione separata
([Dati dimostrativi](/installazione/dati-demo/)).
