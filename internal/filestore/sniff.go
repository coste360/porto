package filestore

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	_ "image/png"  // registers the PNG decoder for image.DecodeConfig
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Content types known to the file store.
const (
	TypePNG  = "image/png"
	TypeJPEG = "image/jpeg"
	TypeSVG  = "image/svg+xml"
	TypePDF  = "application/pdf"
)

// MaxImageSide is the largest width or height in pixels accepted for PNG and JPEG images.
const MaxImageSide = 16384

// Sniff errors. Callers map ErrUnsupportedType to unsupported_media_type and the others to invalid_file.
var (
	ErrUnsupportedType = errors.New("filestore: unsupported content type")
	ErrTypeMismatch    = errors.New("filestore: content does not match the declared type")
	ErrInvalidImage    = errors.New("filestore: invalid or oversized image")
	ErrUnsafeSVG       = errors.New("filestore: SVG with scripts, handlers or external content")
)

var (
	svgRoot      = regexp.MustCompile(`(?is)^(?:\x{FEFF})?\s*(?:<\?xml[^>]*\?>\s*)?(?:<!--.*?-->\s*)*<svg[\s>]`)
	svgUnsafe    = regexp.MustCompile(`(?i)<\s*script|<\s*foreignObject|<!ENTITY|<!DOCTYPE|javascript:|\son[a-z]+\s*=|<\s*(?:iframe|embed|object)\b|(?:href|src)\s*=\s*["']\s*(?:https?:|//|data:)`)
	svgViewBox   = regexp.MustCompile(`(?is)<svg[^>]*\sviewBox\s*=\s*["']\s*[-\d.]+[\s,]+[-\d.]+[\s,]+(\d+)(?:\.\d*)?[\s,]+(\d+)(?:\.\d*)?\s*["']`)
	svgWidthAttr = regexp.MustCompile(`(?is)<svg[^>]*\swidth\s*=\s*["']\s*(\d+)`)
	svgHeightAtt = regexp.MustCompile(`(?is)<svg[^>]*\sheight\s*=\s*["']\s*(\d+)`)
)

// Sniff detects the type of a file from its content and checks it against the declared Content-Type
// (parameters ignored). content is the whole file (at most MaxFileBytes): JPEG dimensions may sit far from the
// start. It returns the detected type and, for images, the size in pixels (SVG: from viewBox, else
// width/height, else 0). PNG and JPEG must decode and stay within MaxImageSide per side; an SVG must have an
// <svg> root and no scripts, event handlers, entities or external references.
func Sniff(content []byte, declared string) (contentType string, w, h int, err error) {
	want, _, perr := mime.ParseMediaType(declared)
	if perr != nil {
		want = strings.ToLower(strings.TrimSpace(declared))
	}
	switch want {
	case TypePNG, TypeJPEG, TypeSVG, TypePDF:
	default:
		return "", 0, 0, ErrUnsupportedType
	}
	got := detect(content)
	if got == "" {
		return "", 0, 0, ErrTypeMismatch
	}
	if got != want {
		return "", 0, 0, ErrTypeMismatch
	}
	switch got {
	case TypePNG, TypeJPEG:
		cfg, _, derr := image.DecodeConfig(bytes.NewReader(content))
		if derr != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxImageSide || cfg.Height > MaxImageSide {
			return "", 0, 0, ErrInvalidImage
		}
		return got, cfg.Width, cfg.Height, nil
	case TypeSVG:
		if svgUnsafe.Match(content) {
			return "", 0, 0, ErrUnsafeSVG
		}
		w, h = svgSize(content)
		return got, w, h, nil
	}
	return got, 0, 0, nil
}

func detect(content []byte) string {
	switch ct := http.DetectContentType(content); {
	case ct == TypePNG, ct == TypeJPEG, ct == TypePDF:
		return ct
	case strings.HasPrefix(ct, "text/xml"), strings.HasPrefix(ct, "text/plain"), strings.HasPrefix(ct, "image/svg+xml"):
		if svgRoot.Match(content) {
			return TypeSVG
		}
	}
	return ""
}

func svgSize(content []byte) (w, h int) {
	if m := svgViewBox.FindSubmatch(content); m != nil {
		return atoi(m[1]), atoi(m[2])
	}
	if mw, mh := svgWidthAttr.FindSubmatch(content), svgHeightAtt.FindSubmatch(content); mw != nil && mh != nil {
		return atoi(mw[1]), atoi(mh[1])
	}
	return 0, 0
}

func atoi(b []byte) int {
	n, err := strconv.Atoi(string(b))
	if err != nil || n < 0 || n > MaxImageSide*16 {
		return 0
	}
	return n
}
