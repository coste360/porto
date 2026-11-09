# Verbale di {{handover.phase}} {{rental.number}}

{{logo}}

Noleggiatore **{{renter.display_name}}**, unità **{{rental.unit_code}}** ({{vessel.name}}), formula {{rental.formula}}, dal {{rental.start}} al {{rental.end}}.

Momento effettivo: **{{handover.at}}**{{#if handover.operator}}, operatore {{handover.operator}}{{/if}}.

Carburante: {{handover.fuel_eighths}}/8. Ore motore: {{handover.engine_hours}}. Foto: {{handover.photos}}.

### Controlli

| Voce | Esito | Nota |
|---|---|---|
{{#each checklist}}| {{this.label}} | {{#if this.ok}}OK{{/if}} | {{this.note}} |
{{/each}}

{{#if damages}}
### Danni

| Descrizione | Preventivo | Stato |
|---|---:|---|
{{#each damages}}| {{this.description}} | {{this.estimate | money}} | {{this.status}} |
{{/each}}
{{/if}}

{{#if late_minutes}}Ritardo: {{late_minutes}} minuti, supplemento {{late_fee | money}}.{{/if}}
{{#if fuel}}Carburante addebitato: {{fuel | money}}.{{/if}}
{{#if return_total}}Addebito al rientro: **{{return_total | money}}**.{{/if}}

{{#if deposit}}Cauzione {{deposit | money}}{{#if deposit_retained}}: trattenuti {{deposit_retained | money}}{{/if}}{{#if deposit_released}}, restituiti {{deposit_released | money}}{{/if}}.{{/if}}

{{#if handover.notes}}
Note: {{handover.notes}}
{{/if}}
