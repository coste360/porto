# Holder chain of berth {{berth.code}}

{{#each entries}}
## {{this.right.from_on | date}} – {{this.right.to_on | date "short" | default "open"}} ({{this.status}})

{{#each this.holders}}- {{this.party.display_name}}: {{this.share | millis}}
{{/each}}
{{/each}}
