// Command mkico writes a Windows .ico from PNG files of the brand kit (assets/brand/porto/05_PNG_Icone), without
// ImageMagick: every PNG is stored as it is (PNG-compressed icon entries, Windows Vista and later), so the icon
// pixels are exactly the kit's (ADR 0007: never redrawn).
//
//	go run ./scripts/desktop/mkico -o porto.ico porto_app_navy_64.png porto_app_navy_128.png porto_app_navy_256.png
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image/png"
	"io"
	"os"
)

func main() {
	out := flag.String("o", "", "output .ico file")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: mkico -o FILE.ico IMAGE.png...")
		os.Exit(2)
	}
	var images [][]byte
	for _, p := range flag.Args() {
		b, err := os.ReadFile(p) //nolint:gosec // G304: developer tool reading the given kit files
		if err != nil {
			fmt.Fprintln(os.Stderr, "mkico:", err)
			os.Exit(1)
		}
		images = append(images, b)
	}
	var buf bytes.Buffer
	if err := Write(&buf, images); err != nil {
		fmt.Fprintln(os.Stderr, "mkico:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306: a build artefact
		fmt.Fprintln(os.Stderr, "mkico:", err)
		os.Exit(1)
	}
}

// Write writes an ICO with one PNG entry per image (square, at most 256 px).
func Write(w io.Writer, images [][]byte) error {
	if len(images) == 0 || len(images) > 255 {
		return errors.New("mkico: 1 to 255 images")
	}
	type entry struct {
		size int
		data []byte
	}
	entries := make([]entry, 0, len(images))
	for i, b := range images {
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return fmt.Errorf("mkico: image %d: %w", i, err)
		}
		if cfg.Width != cfg.Height || cfg.Width > 256 || cfg.Width < 1 {
			return fmt.Errorf("mkico: image %d is %dx%d (square, at most 256 px)", i, cfg.Width, cfg.Height)
		}
		entries = append(entries, entry{cfg.Width, b})
	}
	// ICONDIR: reserved, type 1 (icon), count.
	hdr := []uint16{0, 1, uint16(len(entries))} //nolint:gosec // G115: at most 255 entries
	if err := binary.Write(w, binary.LittleEndian, hdr); err != nil {
		return err
	}
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		dim := byte(e.size % 256) // 256 is written as 0
		dir := struct {
			Width, Height, Colors, Reserved byte
			Planes, BitCount                uint16
			Size, Offset                    uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(e.data)), uint32(offset)} //nolint:gosec // G115: kit PNGs are small
		if err := binary.Write(w, binary.LittleEndian, dir); err != nil {
			return err
		}
		offset += len(e.data)
	}
	for _, e := range entries {
		if _, err := w.Write(e.data); err != nil {
			return err
		}
	}
	return nil
}

// Read returns the PNG payloads of an ICO written by Write (tests).
func Read(b []byte) ([][]byte, error) {
	if len(b) < 6 || binary.LittleEndian.Uint16(b[2:4]) != 1 {
		return nil, errors.New("mkico: not an icon")
	}
	n := int(binary.LittleEndian.Uint16(b[4:6]))
	var out [][]byte
	for i := 0; i < n; i++ {
		d := 6 + 16*i
		if d+16 > len(b) {
			return nil, errors.New("mkico: truncated")
		}
		size := int(binary.LittleEndian.Uint32(b[d+8 : d+12]))
		off := int(binary.LittleEndian.Uint32(b[d+12 : d+16]))
		if off < 0 || size < 0 || off+size > len(b) {
			return nil, errors.New("mkico: bad entry")
		}
		out = append(out, b[off:off+size])
	}
	return out, nil
}
