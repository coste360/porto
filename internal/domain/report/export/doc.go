// Package export turns a report result into one Table (columns labelled in the user's locale, rows from the
// same records as the JSON) and writes it as Italian CSV (UTF-8 BOM, ';', CRLF, decimal comma, dates
// GG/MM/AAAA, formula-injection guard) or as an Excel workbook through internal/xlsx (m7-plan §7, INV-63).
//
// It is pure: no store, no clock, no randomness, no float (depguard pure-engines).
package export
