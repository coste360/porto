# Signature evidence page

Document: **{{document_title}}**

SHA-256 fingerprint of the signed document: {{document_sha256}}

Signature request: {{request_id}}

Simple electronic signature with a one-time code sent by email. The legal value of this kind of signature is for the marina and its advisers to assess.

{{#each signers}}
## Signer {{@index}}: {{this.name}} ({{this.role}})

- Declared identity: {{this.declared_identity}}
- Email fingerprint: {{this.email_hash}}
- IP address: {{this.ip}}
- Browser fingerprint: {{this.user_agent_sha256}}
- Document opened: {{this.opened_at | date}}
- Code sent: {{this.otp_sent_at | date}}
- Signed: **{{this.signed_at | date}}**
{{/each}}
