# Spese comuni {{year}} ({{kind}})

**{{holder.display_name}}**

| Voce | Tabella | Posto | Millesimi | Importo voce | Quota posto | Tua quota |
|---|---|---|---:|---:|---:|---:|
{{#each lines}}| {{this.item}} | {{this.table}} | {{this.berth}} | {{this.millis | millis}} | {{this.item_amount | money}} | {{this.berth_share | money}} | {{this.holder_amount | money}} |
{{/each}}

Totale: **{{total | money}}**
