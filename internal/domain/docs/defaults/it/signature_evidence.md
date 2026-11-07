# Pagina delle evidenze di firma

Documento: **{{document_title}}**

Impronta SHA-256 del documento firmato: {{document_sha256}}

Richiesta di firma: {{request_id}}

Firma elettronica semplice con codice monouso inviato per email. Il valore legale di questa forma di firma va valutato dal porto e dai suoi consulenti.

{{#each signers}}
## Firmatario {{@index}}: {{this.name}} ({{this.role}})

- Identità dichiarata: {{this.declared_identity}}
- Impronta dell'email: {{this.email_hash}}
- Indirizzo IP: {{this.ip}}
- Impronta del browser: {{this.user_agent_sha256}}
- Apertura del documento: {{this.opened_at | date}}
- Invio del codice: {{this.otp_sent_at | date}}
- Firma: **{{this.signed_at | date}}**
{{/each}}
