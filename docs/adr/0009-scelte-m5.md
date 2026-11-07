# ADR 0009 — Scelte di M5 (addebiti e incassi)

- **Stato:** proposta
- **Data:** 2026-11-07
- **Sezioni della specifica:** 06.7, 16, 26, 27, 30, 32 (M5), 33 (D4)

## Contesto

La milestone M5 introduce addebiti da ogni origine, incassi e rimborsi con abbinamento, crediti, cassa e
corrispettivi, solleciti, pagamenti online e l'esportazione "da fatturare". Su alcuni punti la specifica tace o
descrive una struttura diversa da quella realizzata; le decisioni prese sono elencate in `docs/m5-plan.md` §16
(D1–D28). Questo ADR registra quelle che cambiano la specifica o che vincolano le milestone successive.

## Decisione

1. **Credito derivato.** Il credito di un cliente è la somma del denaro non abbinato dei suoi pagamenti
   (`payments.allocated_cents < amount_cents`); i crediti non in denaro (resto di una rettifica negativa, saldo di
   apertura negativo, omaggio, rilascio, mandato) sono righe `payments.kind = credit`, mai in cassa né nei
   corrispettivi. La tabella `customer_credits` della specifica 06.7 non esiste (D5, come Arenile M5).
2. **Pagamenti offline prima di tutto.** Ogni flusso funziona con il solo driver manuale; Stripe (con SEPA) è un
   driver facoltativo dietro `pkg/ext.PaymentProvider`, disattivato di default e configurato dal porto, chiamato via
   HTTPS dal client di egress senza SDK (D11). I pagamenti in eccesso o tardivi diventano credito e voce di lavoro,
   mai rimborsi automatici (D14); le contestazioni (chargeback) del provider sono registrate come eventi ignorati con
   una voce di lavoro `dispute`.
3. **Nessuna fatturazione nel nucleo.** Gli addebiti si esportano in un CSV/Excel generico (nessun software di
   fatturazione scelto, decisione aperta D4 della sezione 33); gli addebiti esportati sono immutabili e si
   correggono con addebiti di rettifica collegati. I file di un'esportazione sono riproducibili byte per byte: i dati
   delle righe che non sono già immutabili sono congelati sulle voci dell'esportazione (D27).
4. **Denaro mancante sempre visibile.** Quando un addebito non si può creare (obiettivo del contratto incoerente,
   articolo d'origine non configurato) l'operazione prosegue senza addebito e apre una voce di lavoro
   (`billing_target_mismatch`, `billing_item_missing`) nella stessa transazione (D26).
5. Le altre scelte di `docs/m5-plan.md` §16 (D1–D28) valgono come descritte lì.

## Conseguenze

- La specifica 06.7 va letta con il punto 1; un modulo che volesse uno storico dei crediti separato richiede un
  nuovo ADR.
- Porto resta utilizzabile senza internet; un nuovo gateway è un driver (`/new-payment-driver`), non una modifica
  del nucleo.
- La scelta del software di fatturazione (sezione 33, D4) resta aperta e si implementa come `ChargeExporter`.
