// Package remote is the remote mode of the desktop app (m6b-plan §7): a loopback reverse proxy to an online Porto
// instance that holds the per-device token (never shown to the page, INV-85), pins the instance's certificate
// (leaf, then its CA), caches the panel shell for an offline start, answers the remote subset of /api/desktop/v1
// and /api/bridge/v1 locally (app PIN for administration) and runs the fiscal bridge agent. It runs without a
// database or an app.App: it imports neither internal/store nor internal/app (depguard).
package remote
