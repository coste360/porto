# Riepilogo esportazione addebiti {{number}}

Esportazione {{#if kind == "reexport"}}ripetuta{{else}}da fatturare{{/if}} del periodo {{period_from | date}} – {{period_to | date}}{{#if filters}} ({{filters}}){{/if}}.

Addebiti: **{{charge_count}}** · righe: {{line_count}} · imponibile {{net | money}} · IVA {{vat_total | money}} · totale **{{total | money}}**

Gli addebiti sono riferimenti interni di Porto: le fatture sono emesse dal software di fatturazione.

## Riepilogo IVA

| Aliquota | Natura | Imponibile | IVA | Totale |
|---:|---|---:|---:|---:|
{{#each vat}}| {{this.vat_rate | bp}} | {{this.vat_nature}} | {{this.net | money}} | {{this.vat | money}} | {{this.gross | money}} |
{{/each}}

## Per origine

| Origine | Addebiti | Totale |
|---|---:|---:|
{{#each by_origin}}| {{this.key}} | {{this.count}} | {{this.gross | money}} |
{{/each}}

## Per classe di ricavo

| Classe | Righe | Totale |
|---|---:|---:|
{{#each by_revenue_class}}| {{this.key}} | {{this.count}} | {{this.gross | money}} |
{{/each}}

## Addebiti

| Numero | Data | Cliente | Origine | Imponibile | IVA | Totale | Rettifica di |
|---|---|---|---|---:|---:|---:|---|
{{#each charges}}| {{this.number}} | {{this.issued_on | date}} | {{this.customer}} | {{this.origin_type}} | {{this.net | money}} | {{this.vat | money}} | {{this.total | money}} | {{this.corrects}} |
{{/each}}
