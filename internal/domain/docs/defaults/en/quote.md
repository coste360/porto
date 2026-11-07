# Quote no. {{quote.code}}

{{logo}}

Dear **{{customer.display_name}}**, we offer the mooring of the vessel **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}, length {{vessel.loa | meters}}, beam {{vessel.beam | meters}}** from {{quote.starts_on | date "long"}} to {{quote.ends_on | date "long"}}.

{{#each options}}
### Option {{this.seq}}{{#if this.label}}: {{this.label}}{{/if}}

| Berth | Structure | From | To |
|---|---|---|---|
{{#each this.placements}}| {{this.code}} | {{this.structure}} | {{this.first_night | date}} | {{this.last_night | date}} |
{{/each}}

| Item | Amount |
|---|---:|
{{#each this.price.lines}}| {{this.description}} | {{this.amount | money}} |
{{/each}}

Total **{{this.price.total | money}}**{{#if this.price.deposit}}, deposit on signature {{this.price.deposit | money}}{{/if}}.
{{/each}}

This quote is valid until {{valid_until | date "long"}}.

{{#if link}}You can accept it online: {{link}}{{/if}}

{{#if quote.notes}}
{{quote.notes}}
{{/if}}
