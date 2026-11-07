-- Porto M6a device sessions (PostgreSQL): a browser paired by QR on the local network authenticates with a session
-- bound to its device (m6a-plan §3.5). The twin is internal/migrations/sqlite/00013_device_sessions.sql: keep
-- column, index and constraint names identical.
--
-- sessions.device_id: NULL for ordinary logins; set for device sessions (idle 30 days, absolute 180 days), which
--                     disappear with their device (revoking a device deletes its sessions in the same transaction).
-- devices.access:     'token' (po_dev_ device token, native clients) or 'browser' (device session cookie; the
--                     stored token hash is random and never returned).

-- +goose Up
ALTER TABLE sessions ADD COLUMN device_id UUID REFERENCES devices (id) ON DELETE CASCADE;
CREATE INDEX sessions_device_id_idx ON sessions (device_id);

ALTER TABLE devices ADD COLUMN access TEXT NOT NULL DEFAULT 'token' CHECK (access IN ('token','browser'));

-- +goose Down
ALTER TABLE devices DROP COLUMN access;

DROP INDEX sessions_device_id_idx;
ALTER TABLE sessions DROP COLUMN device_id;
