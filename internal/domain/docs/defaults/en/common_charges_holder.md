# Common charges {{year}} ({{kind}})

**{{holder.display_name}}**

| Item | Table | Berth | Millesimal | Item amount | Berth share | Your share |
|---|---|---|---:|---:|---:|---:|
{{#each lines}}| {{this.item}} | {{this.table}} | {{this.berth}} | {{this.millis | millis}} | {{this.item_amount | money}} | {{this.berth_share | money}} | {{this.holder_amount | money}} |
{{/each}}

Total: **{{total | money}}**
