// Package xlsx is Porto's own minimal XLSX writer and first-sheet reader (m5-plan §16 D18): inline strings,
// decimal numbers built from integer cents (never float64), dates, bold header, column widths; the reader is
// bounded (uncompressed size, cells, zip-bomb ratio). Standard library only, on io.Reader / io.Writer. It is pure
// (depguard pure-engines). Owned by CHARGES-EXPORT.
package xlsx
