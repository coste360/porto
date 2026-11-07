# Rendiconto del mandato di gestione

**{{holder.display_name}}** – {{right.berth.code}}, {{from | date}} – {{to | date}} ({{owner_share_bp | bp}})

| Contratto | Notti | Ricavo | Quota titolare |
|---|---:|---:|---:|
{{#each lines}}| {{this.contract_code}} | {{this.nights}} | {{this.revenue | money}} | {{this.owner_share | money}} |
{{/each}}

- Ricavo: {{revenue | money}}
- Quota titolare: **{{owner_share | money}}**
- Integrazione minimo garantito: {{top_up | money}}
