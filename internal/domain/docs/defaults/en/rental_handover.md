# Handover report: {{handover.phase}} {{rental.number}}

{{logo}}

Renter **{{renter.display_name}}**, unit **{{rental.unit_code}}** ({{vessel.name}}), formula {{rental.formula}}, from {{rental.start}} to {{rental.end}}.

Actual time: **{{handover.at}}**{{#if handover.operator}}, operator {{handover.operator}}{{/if}}.

Fuel: {{handover.fuel_eighths}}/8. Engine hours: {{handover.engine_hours}}. Photos: {{handover.photos}}.

### Checks

| Item | Outcome | Note |
|---|---|---|
{{#each checklist}}| {{this.label}} | {{#if this.ok}}OK{{/if}} | {{this.note}} |
{{/each}}

{{#if damages}}
### Damages

| Description | Estimate | Status |
|---|---:|---|
{{#each damages}}| {{this.description}} | {{this.estimate | money}} | {{this.status}} |
{{/each}}
{{/if}}

{{#if late_minutes}}Late by {{late_minutes}} minutes, late fee {{late_fee | money}}.{{/if}}
{{#if fuel}}Fuel charged: {{fuel | money}}.{{/if}}
{{#if return_total}}Return charge: **{{return_total | money}}**.{{/if}}

{{#if deposit}}Deposit {{deposit | money}}{{#if deposit_retained}}: retained {{deposit_retained | money}}{{/if}}{{#if deposit_released}}, released {{deposit_released | money}}{{/if}}.{{/if}}

{{#if handover.notes}}
Notes: {{handover.notes}}
{{/if}}
