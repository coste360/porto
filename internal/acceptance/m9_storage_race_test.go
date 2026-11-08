//go:build integration && race

package acceptance

// storageRace reports a -race build: the M9 timing assertions are skipped under it.
const storageRace = true
