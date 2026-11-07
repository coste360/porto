# Oneri di gestione {{year}}

**{{holder.display_name}}** – posto {{right.berth.code}}

- Base: {{base | money}}{{#if istat}}, adeguamento ISTAT {{istat | bp}}: {{updated | money}}{{/if}}
- Giorni nell'anno: {{days}}
- Importo del posto: {{amount | money}}
- Tua quota ({{share | millis}}): **{{holder_amount | money}}**

| Rata | Scadenza | Importo |
|---:|---|---:|
{{#each installments}}| {{this.seq}} | {{this.due_on | date}} | {{this.amount | money}} |
{{/each}}
