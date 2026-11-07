# Cessione del diritto d'uso del posto {{right.berth.code}}

Con effetto dal {{effective_on | date "long"}} il diritto d'uso ({{right.kind}}) sul posto **{{right.berth.code}}** passa dai titolari cedenti ai titolari cessionari indicati.

## Cedenti

{{#each from}}- {{this.party.display_name}}{{#if this.party.fiscal_code}}, C.F. {{this.party.fiscal_code}}{{/if}}: {{this.share | millis}}
{{/each}}

## Cessionari

{{#each to}}- {{this.party.display_name}}{{#if this.party.fiscal_code}}, C.F. {{this.party.fiscal_code}}{{/if}}: {{this.share | millis}}
{{/each}}

{{#if declared_price}}Prezzo dichiarato: {{declared_price | money}}.{{/if}} {{#if takeover_fee}}Diritto di subentro: {{takeover_fee | money}}.{{/if}}

{{signature "outgoing_holder"}}

{{signature "incoming_holder"}}

{{signature "marina"}}
