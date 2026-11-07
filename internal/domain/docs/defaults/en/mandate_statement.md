# Management mandate statement

**{{holder.display_name}}** – {{right.berth.code}}, {{from | date}} – {{to | date}} ({{owner_share_bp | bp}})

| Contract | Nights | Revenue | Holder share |
|---|---:|---:|---:|
{{#each lines}}| {{this.contract_code}} | {{this.nights}} | {{this.revenue | money}} | {{this.owner_share | money}} |
{{/each}}

- Revenue: {{revenue | money}}
- Holder share: **{{owner_share | money}}**
- Guaranteed minimum top-up: {{top_up | money}}
