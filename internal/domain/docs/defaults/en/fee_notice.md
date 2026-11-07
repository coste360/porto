# Management fees {{year}}

**{{holder.display_name}}** – berth {{right.berth.code}}

- Base: {{base | money}}{{#if istat}}, ISTAT update {{istat | bp}}: {{updated | money}}{{/if}}
- Days in the year: {{days}}
- Berth amount: {{amount | money}}
- Your share ({{share | millis}}): **{{holder_amount | money}}**

| No. | Due | Amount |
|---:|---|---:|
{{#each installments}}| {{this.seq}} | {{this.due_on | date}} | {{this.amount | money}} |
{{/each}}
