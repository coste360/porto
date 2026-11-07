# Transfer of the berth right on berth {{right.berth.code}}

Effective from {{effective_on | date "long"}} the berth right ({{right.kind}}) on berth **{{right.berth.code}}** passes from the outgoing holders to the incoming holders below.

## Outgoing holders

{{#each from}}- {{this.party.display_name}}{{#if this.party.fiscal_code}}, tax code {{this.party.fiscal_code}}{{/if}}: {{this.share | millis}}
{{/each}}

## Incoming holders

{{#each to}}- {{this.party.display_name}}{{#if this.party.fiscal_code}}, tax code {{this.party.fiscal_code}}{{/if}}: {{this.share | millis}}
{{/each}}

{{#if declared_price}}Declared price: {{declared_price | money}}.{{/if}} {{#if takeover_fee}}Takeover fee: {{takeover_fee | money}}.{{/if}}

{{signature "outgoing_holder"}}

{{signature "incoming_holder"}}

{{signature "marina"}}
