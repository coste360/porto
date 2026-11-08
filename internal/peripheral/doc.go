// Package peripheral holds the local peripheral drivers of the desktop app (m6b-plan §8.2): fake drivers for
// development and tests (fake.go), a generic ESC/POS receipt printer (escpos.go), a generic ZPL label printer
// (zpl.go) and the byte transports (transport.go: tcp, cups, file). They implement the interfaces of
// internal/desktopport and pkg/ext.FiscalDriver, are pure Go (CGO_ENABLED=0) and import only the standard
// library, internal/desktopport and pkg/ext (depguard). Build turns the desktop.json peripheral settings into
// drivers. Real fiscal device models are extensions.
//
// The drivers live in one package (not one per driver as sketched in m6b-plan §8.2) so that depguard's
// "standard library, desktopport and pkg/ext only" rule needs no exception for sibling packages.
package peripheral
