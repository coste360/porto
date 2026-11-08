# Commessa {{order.number}}

{{logo}}

Cliente **{{customer.display_name}}**, unità **{{vessel.name}}{{#if vessel.registration}} ({{vessel.registration}}){{/if}}**, lunghezza {{vessel.loa | meters}}.

{{#if order.quote_number}}Dal preventivo **{{order.quote_number}}**.{{/if}} Stato: {{order.status}}, avanzamento {{order.completion | bp}}.
{{#if order.position}}Posizione: {{order.position}}.{{/if}}

{{#if order.planned_start_on}}Inizio previsto il {{order.planned_start_on | date "long"}}{{#if order.planned_delivery_on}}, consegna prevista il {{order.planned_delivery_on | date "long"}}{{/if}}.{{/if}}

### Lavori previsti

| # | Descrizione | Q.tà | Prezzo | Imponibile |
|---:|---|---:|---:|---:|
{{#each lines}}| {{this.seq}} | {{this.description}} | {{this.qty}} | {{this.unit_price | money}} | {{this.net | money}} |
{{/each}}

Imponibile previsto {{net | money}}.

### Attività

| # | Attività | Ore stimate | Avanzamento | Stato |
|---:|---|---:|---:|---|
{{#each tasks}}| {{this.seq}} | {{this.title}} | {{this.estimated_hours}} | {{this.progress | bp}} | {{this.status}} |
{{/each}}

{{#if charged}}Già addebitato: {{charged | money}}.{{/if}}

{{#if order.notes}}
{{order.notes}}
{{/if}}
