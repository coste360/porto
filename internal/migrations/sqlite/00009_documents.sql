-- Porto M4 documents schema (SQLite): print templates and their versions, generated documents, signature
-- requests, signers and OTP codes, customer access links (magic links), communications and the per-customer message
-- registry, signature provider settings and CSV import runs (spec 06, 15, 28, 11.6; m4-plan §5.3). The twin is
-- internal/migrations/postgres/00009_documents.sql: keep table, column, index and constraint names identical.
--
-- Generated and signed PDFs, scans and evidence are stored_files rows (purpose 'document', encrypted by
-- internal/filestore); *_enc columns are KeyRing ciphertexts. Published template versions and signed documents are
-- immutable (triggers); one open signature request per subject (signature_requests_open_uidx).

-- +goose Up
CREATE TABLE templates (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                TEXT NOT NULL,                        -- docs catalogue kind (validated in Go)
    code                TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 64),
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    is_default          INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1)),
    archived_at         TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE UNIQUE INDEX templates_code_uidx ON templates (marina_id, code);
CREATE INDEX templates_marina_kind_idx ON templates (marina_id, kind);

CREATE TABLE template_versions (
    id                    TEXT PRIMARY KEY,
    marina_id             TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    template_id           TEXT NOT NULL,
    version               INTEGER NOT NULL CHECK (version >= 1),
    locale                TEXT NOT NULL CHECK (length(locale) BETWEEN 2 AND 8),
    body_md               TEXT NOT NULL DEFAULT '',
    header_md             TEXT NOT NULL DEFAULT '',
    footer_md             TEXT NOT NULL DEFAULT '',
    page                  TEXT NOT NULL DEFAULT '{}',         -- {margins_mm, font_size_pt}
    status                TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','superseded')),
    sha256                TEXT,                               -- of body, header, footer and page (set at publish)
    published_at          TIMESTAMP,
    published_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_by_user_id    TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at            TIMESTAMP NOT NULL,
    updated_at            TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (template_id, marina_id) REFERENCES templates (id, marina_id) ON DELETE CASCADE,
    CONSTRAINT template_versions_published_check CHECK (status = 'draft' OR (sha256 IS NOT NULL AND published_at IS NOT NULL))
);
CREATE UNIQUE INDEX template_versions_uidx ON template_versions (template_id, version, locale);
CREATE UNIQUE INDEX template_versions_published_uidx ON template_versions (template_id, locale) WHERE status = 'published';
CREATE INDEX template_versions_marina_idx ON template_versions (marina_id, template_id);

CREATE TABLE generated_documents (
    id                   TEXT PRIMARY KEY,
    marina_id            TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    kind                 TEXT NOT NULL,                       -- docs catalogue kind (validated in Go)
    template_version_id  TEXT,
    builtin_ref          TEXT,                                -- built-in default "<locale>/<kind>@<sha256>"
    locale               TEXT NOT NULL DEFAULT 'it',
    subject_type         TEXT NOT NULL CHECK (subject_type IN ('quote','contract','addendum','renewal_proposal',
                           'right_transfer','holder_chain','mandate_statement','fee_notice','common_charges_holder',
                           'common_charges_items','installment_notice','communication','map_print','letter')),
    subject_id           TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','rendering','ready','failed','superseded')),
    data_enc             TEXT,                                -- KeyRing, AAD docdata:<id>
    file_id              TEXT,
    sha256               TEXT,                                -- of the plaintext PDF
    pages                INTEGER NOT NULL DEFAULT 0 CHECK (pages >= 0),
    pdfa                 INTEGER NOT NULL DEFAULT 0 CHECK (pdfa IN (0,1)),
    signed               INTEGER NOT NULL DEFAULT 0 CHECK (signed IN (0,1)),
    signed_from_id       TEXT,
    attempts             INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at      TIMESTAMP,
    lease_until          TIMESTAMP,
    error_code           TEXT,
    rendered_at          TIMESTAMP,
    created_by_user_id   TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at           TIMESTAMP NOT NULL,
    updated_at           TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (template_version_id, marina_id) REFERENCES template_versions (id, marina_id),
    FOREIGN KEY (file_id, marina_id) REFERENCES stored_files (id, marina_id),
    FOREIGN KEY (signed_from_id, marina_id) REFERENCES generated_documents (id, marina_id) DEFERRABLE INITIALLY DEFERRED,
    CONSTRAINT generated_documents_source_check CHECK (template_version_id IS NOT NULL OR builtin_ref IS NOT NULL),
    CONSTRAINT generated_documents_ready_check CHECK (status <> 'ready' OR (file_id IS NOT NULL AND sha256 IS NOT NULL)),
    CONSTRAINT generated_documents_signed_check CHECK (signed = 0 OR signed_from_id IS NOT NULL)
);
CREATE INDEX generated_documents_subject_idx ON generated_documents (marina_id, subject_type, subject_id);
CREATE INDEX generated_documents_pending_idx ON generated_documents (status, lease_until) WHERE status IN ('pending','rendering');
CREATE INDEX generated_documents_signed_from_idx ON generated_documents (signed_from_id);

CREATE TABLE signature_requests (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    subject_type        TEXT NOT NULL CHECK (subject_type IN ('contract','addendum','right_transfer')),
    subject_id          TEXT NOT NULL,
    document_id         TEXT NOT NULL,
    driver              TEXT NOT NULL CHECK (driver IN ('paper','otp','provider')),
    provider_code       TEXT,
    envelope_ref        TEXT,
    order_mode          TEXT NOT NULL DEFAULT 'parallel' CHECK (order_mode IN ('sequential','parallel')),
    status              TEXT NOT NULL DEFAULT 'pending_document' CHECK (status IN ('pending_document','sent',
                          'partially_signed','signed','declined','expired','cancelled')),
    expires_at          TIMESTAMP NOT NULL,
    last_reminder_at    TIMESTAMP,
    reminders_sent      INTEGER NOT NULL DEFAULT 0 CHECK (reminders_sent >= 0),
    signed_document_id  TEXT,
    completed_at        TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (document_id, marina_id) REFERENCES generated_documents (id, marina_id),
    FOREIGN KEY (signed_document_id, marina_id) REFERENCES generated_documents (id, marina_id),
    CONSTRAINT signature_requests_provider_check CHECK ((driver = 'provider') = (provider_code IS NOT NULL)),
    CONSTRAINT signature_requests_signed_check CHECK (status <> 'signed' OR signed_document_id IS NOT NULL)
);
CREATE UNIQUE INDEX signature_requests_open_uidx ON signature_requests (subject_type, subject_id)
    WHERE status IN ('pending_document','sent','partially_signed');
CREATE INDEX signature_requests_marina_idx ON signature_requests (marina_id, status, expires_at);
CREATE INDEX signature_requests_subject_idx ON signature_requests (marina_id, subject_type, subject_id);

CREATE TABLE signature_signers (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    request_id          TEXT NOT NULL,
    seq                 INTEGER NOT NULL CHECK (seq >= 1),
    role                TEXT NOT NULL CHECK (role IN ('customer','co_holder','outgoing_holder','incoming_holder',
                          'legal_representative','marina')),
    customer_id         TEXT,
    user_id             TEXT REFERENCES users (id) ON DELETE SET NULL,
    display_name_enc    TEXT NOT NULL,                        -- KeyRing, AAD signer:<id>
    email_enc           TEXT,                                 -- KeyRing, AAD signer:<id>
    email_hash          TEXT,
    locale              TEXT NOT NULL DEFAULT 'it',
    status              TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting','invited','signed','declined')),
    link_id             TEXT,                                  -- access_links.id
    invited_at          TIMESTAMP,
    opened_at           TIMESTAMP,
    signed_at           TIMESTAMP,
    declined_at         TIMESTAMP,
    evidence_enc        TEXT,                                 -- KeyRing, AAD evidence:<id>
    paper_scan_file_id  TEXT,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (request_id, marina_id) REFERENCES signature_requests (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id),
    FOREIGN KEY (paper_scan_file_id, marina_id) REFERENCES stored_files (id, marina_id)
);
CREATE UNIQUE INDEX signature_signers_seq_uidx ON signature_signers (request_id, seq);
CREATE INDEX signature_signers_marina_idx ON signature_signers (marina_id, request_id);

CREATE TABLE signature_otps (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    signer_id    TEXT NOT NULL,
    code_hmac    TEXT NOT NULL,
    expires_at   TIMESTAMP NOT NULL,
    attempts     INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
    consumed_at  TIMESTAMP,
    sent_at      TIMESTAMP,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    FOREIGN KEY (signer_id, marina_id) REFERENCES signature_signers (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX signature_otps_signer_idx ON signature_otps (signer_id, created_at);
CREATE INDEX signature_otps_marina_idx ON signature_otps (marina_id, created_at);

CREATE TABLE access_links (
    id            TEXT PRIMARY KEY,
    marina_id     TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    purpose       TEXT NOT NULL CHECK (purpose IN ('quote','renewal','signature','unsubscribe')),
    subject_type  TEXT NOT NULL,
    subject_id    TEXT NOT NULL,
    customer_id   TEXT,
    token_hash    TEXT NOT NULL,                              -- SHA-256 of the token, never the token
    expires_at    TIMESTAMP NOT NULL,
    revoked_at    TIMESTAMP,
    last_used_at  TIMESTAMP,
    use_count     INTEGER NOT NULL DEFAULT 0 CHECK (use_count >= 0),
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX access_links_token_uidx ON access_links (token_hash);
CREATE INDEX access_links_subject_idx ON access_links (marina_id, subject_type, subject_id);
CREATE INDEX access_links_expiry_idx ON access_links (marina_id, expires_at);

CREATE TABLE comm_campaigns (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    name                TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    category            TEXT NOT NULL CHECK (category IN ('service','promotional')),
    audience            TEXT NOT NULL DEFAULT '{}',           -- filter, customer_ids or {map_id, resource_ids}
    channels            TEXT NOT NULL DEFAULT '["email"]',
    template_id         TEXT,
    subject             TEXT NOT NULL DEFAULT '',
    body_md             TEXT NOT NULL DEFAULT '',
    attachments         TEXT NOT NULL DEFAULT '[]',           -- [{document_id} | {file_id}]
    scheduled_at        TIMESTAMP,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','scheduled','sending','sent','cancelled')),
    counts              TEXT NOT NULL DEFAULT '{}',
    sent_at             TIMESTAMP,
    cancelled_at        TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (template_id, marina_id) REFERENCES templates (id, marina_id),
    CONSTRAINT comm_campaigns_schedule_check CHECK (status <> 'scheduled' OR scheduled_at IS NOT NULL)
);
CREATE INDEX comm_campaigns_marina_idx ON comm_campaigns (marina_id, status, scheduled_at);

CREATE TABLE comm_recipients (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL,
    customer_id  TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','queued','skipped_no_consent','skipped_no_email','failed')),
    message_id   TEXT REFERENCES message_log (id) ON DELETE SET NULL,
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (campaign_id, marina_id) REFERENCES comm_campaigns (id, marina_id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX comm_recipients_uidx ON comm_recipients (campaign_id, customer_id);
CREATE INDEX comm_recipients_marina_idx ON comm_recipients (marina_id, campaign_id, status);

CREATE TABLE customer_messages (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    customer_id  TEXT NOT NULL,
    message_id   TEXT NOT NULL REFERENCES message_log (id) ON DELETE CASCADE,
    category     TEXT NOT NULL CHECK (category IN ('service','promotional','transactional')),
    source_type  TEXT NOT NULL DEFAULT '',
    source_id    TEXT,
    template     TEXT NOT NULL,
    created_at   TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id),
    FOREIGN KEY (customer_id, marina_id) REFERENCES customers (id, marina_id)
);
CREATE UNIQUE INDEX customer_messages_message_uidx ON customer_messages (message_id);
CREATE INDEX customer_messages_customer_idx ON customer_messages (marina_id, customer_id, created_at);

CREATE TABLE signature_settings (
    marina_id           TEXT PRIMARY KEY REFERENCES marinas (id) ON DELETE CASCADE,
    provider_code       TEXT,
    credentials_enc     TEXT,                                 -- KeyRing, AAD signature-credentials:<marina>
    webhook_secret_enc  TEXT,                                 -- KeyRing, AAD signature-webhook:<marina>
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL
);

CREATE TABLE import_runs (
    id                  TEXT PRIMARY KEY,
    marina_id           TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    entity              TEXT NOT NULL CHECK (entity IN ('customers_vessels','contracts','rights','deadlines')),
    status              TEXT NOT NULL CHECK (status IN ('dry_run','applied','undone','failed')),
    mapping             TEXT NOT NULL DEFAULT '{}',           -- saved column mapping, reusable by the next run
    report              TEXT NOT NULL DEFAULT '{}',           -- row numbers and codes only, no personal data
    file_sha256         TEXT NOT NULL,
    row_count           INTEGER NOT NULL DEFAULT 0 CHECK (row_count BETWEEN 0 AND 5000),
    applied_at          TIMESTAMP,
    undone_at           TIMESTAMP,
    created_by_user_id  TEXT REFERENCES users (id) ON DELETE SET NULL,
    version             INTEGER NOT NULL DEFAULT 1,
    created_at          TIMESTAMP NOT NULL,
    updated_at          TIMESTAMP NOT NULL,
    UNIQUE (id, marina_id)
);
CREATE INDEX import_runs_marina_idx ON import_runs (marina_id, created_at);

CREATE TABLE import_run_items (
    id           TEXT PRIMARY KEY,
    marina_id    TEXT NOT NULL REFERENCES marinas (id) ON DELETE CASCADE,
    run_id       TEXT NOT NULL,
    entity_type  TEXT NOT NULL,
    entity_id    TEXT NOT NULL,
    created_at   TIMESTAMP NOT NULL,
    FOREIGN KEY (run_id, marina_id) REFERENCES import_runs (id, marina_id) ON DELETE CASCADE
);
CREATE INDEX import_run_items_run_idx ON import_run_items (marina_id, run_id);

-- Published template versions are frozen except published → superseded; signed documents are frozen; the file and
-- hash of a rendered document never change. The services check first; the triggers are the backstop.
-- +goose StatementBegin
CREATE TRIGGER template_versions_immutable_update BEFORE UPDATE ON template_versions
WHEN OLD.status <> 'draft' AND NOT (
    OLD.status = 'published' AND NEW.status = 'superseded'
    AND NEW.id IS OLD.id AND NEW.marina_id IS OLD.marina_id AND NEW.template_id IS OLD.template_id
    AND NEW.version IS OLD.version AND NEW.locale IS OLD.locale AND NEW.body_md IS OLD.body_md
    AND NEW.header_md IS OLD.header_md AND NEW.footer_md IS OLD.footer_md AND NEW.page IS OLD.page
    AND NEW.sha256 IS OLD.sha256 AND NEW.published_at IS OLD.published_at AND NEW.created_at IS OLD.created_at
)
BEGIN SELECT RAISE(ABORT, 'template_version_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER template_versions_immutable_delete BEFORE DELETE ON template_versions
WHEN OLD.status <> 'draft' AND EXISTS (SELECT 1 FROM templates WHERE id = OLD.template_id)
BEGIN SELECT RAISE(ABORT, 'template_version_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER generated_documents_immutable_update BEFORE UPDATE ON generated_documents
WHEN OLD.signed = 1
    OR (OLD.sha256 IS NOT NULL AND NEW.sha256 IS NOT OLD.sha256)
    OR (OLD.file_id IS NOT NULL AND NEW.file_id IS NOT OLD.file_id)
BEGIN SELECT RAISE(ABORT, 'document_immutable'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER generated_documents_immutable_delete BEFORE DELETE ON generated_documents
WHEN OLD.signed = 1 OR OLD.sha256 IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'document_immutable'); END;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER generated_documents_immutable_delete;
DROP TRIGGER generated_documents_immutable_update;
DROP TRIGGER template_versions_immutable_delete;
DROP TRIGGER template_versions_immutable_update;
DROP TABLE import_run_items;
DROP TABLE import_runs;
DROP TABLE signature_settings;
DROP TABLE customer_messages;
DROP TABLE comm_recipients;
DROP TABLE comm_campaigns;
DROP TABLE access_links;
DROP TABLE signature_otps;
DROP TABLE signature_signers;
DROP TABLE signature_requests;
DROP TABLE generated_documents;
DROP TABLE template_versions;
DROP TABLE templates;
