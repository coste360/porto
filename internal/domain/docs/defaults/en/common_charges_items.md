# Common charges allocation {{year}} ({{kind}})

| Item | Table | Amount | Allocated | Marina share |
|---|---|---:|---:|---:|
{{#each items}}| {{this.item}} | {{this.table}} | {{this.amount | money}} | {{this.allocated | money}} | {{this.marina_share | money}} |
{{/each}}

Total: **{{total | money}}**
