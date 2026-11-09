// Package ingestauth is the pure verification of the M12 ingestion protocol (m12-plan §3.3, §4.2;
// docs/sensor-protocol.md): key format, header parsing, the canonical string, HMAC-SHA256 signatures with a
// timestamp tolerance, bearer comparison. No store, no clock, no randomness: the caller passes the key bytes, the
// random bytes of a new key and now. Comparisons are constant time (crypto/hmac, crypto/subtle; INV-105).
package ingestauth
