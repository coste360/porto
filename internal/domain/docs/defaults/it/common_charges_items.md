# Ripartizione spese comuni {{year}} ({{kind}})

| Voce | Tabella | Importo | Ripartito | Quota marina |
|---|---|---:|---:|---:|
{{#each items}}| {{this.item}} | {{this.table}} | {{this.amount | money}} | {{this.allocated | money}} | {{this.marina_share | money}} |
{{/each}}

Totale: **{{total | money}}**
