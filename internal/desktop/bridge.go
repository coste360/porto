package desktop

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// JS bridge of the window (m6a-plan §2.13): window.portoDesktop = {version, saveFile, openExternal, print}. The
// native bindings have these names; the window build binds them and injects BridgeScript before every page.
const (
	BindSaveFile     = "__portoDesktopSaveFile"
	BindOpenExternal = "__portoDesktopOpenExternal"
	BindPrint        = "__portoDesktopPrint"
)

// MaxSaveFileBytes bounds a file saved through the bridge.
const MaxSaveFileBytes = 50 << 20

// ErrSaveRefused: the bridge refused a file (too large, bad encoding, no Downloads folder).
var ErrSaveRefused = errors.New("desktop: file refused")

// BridgeScript returns the script that defines window.portoDesktop over the native bindings.
func BridgeScript(version string) string {
	v, _ := json.Marshal(version)
	return `(function(){if(window.portoDesktop)return;` +
		`var b=function(n){return function(){var f=window[n];if(typeof f!=="function")return Promise.reject(new Error("bridge unavailable"));return f.apply(null,arguments);};};` +
		`Object.defineProperty(window,"portoDesktop",{value:Object.freeze({version:` + string(v) +
		`,saveFile:b("` + BindSaveFile + `"),openExternal:b("` + BindOpenExternal + `"),print:b("` + BindPrint + `")}),` +
		`writable:false,configurable:false});})();`
}

// DownloadsDir returns the user's Downloads folder (created if missing).
func DownloadsDir(home string) (string, error) {
	if home == "" {
		return "", ErrSaveRefused
	}
	dir := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SanitizeFileName keeps the base name of name with letters, digits, space, dot, dash and underscore only; an
// empty or hidden result becomes "porto-file".
func SanitizeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_' || r == ' ':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if len(out) > 120 {
		ext := filepath.Ext(out)
		if len(ext) > 16 {
			ext = ""
		}
		out = out[:120-len(ext)] + ext
	}
	if out == "" || out == "_" {
		out = "porto-file"
	}
	return out
}

// SaveDownload decodes b64 and writes it into dir under the sanitised name, adding " (n)" before the extension
// when the name is taken. It returns the path written.
func SaveDownload(dir, name, b64 string) (string, error) {
	if base64.StdEncoding.DecodedLen(len(b64)) > MaxSaveFileBytes+3 {
		return "", fmt.Errorf("%w: larger than %d bytes", ErrSaveRefused, MaxSaveFileBytes)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("%w: not base64", ErrSaveRefused)
	}
	if len(data) > MaxSaveFileBytes {
		return "", fmt.Errorf("%w: larger than %d bytes", ErrSaveRefused, MaxSaveFileBytes)
	}
	clean := SanitizeFileName(name)
	ext := filepath.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	for i := 0; i < 1000; i++ {
		candidate := clean
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		path := filepath.Join(dir, candidate)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: sanitised name in Downloads
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return "", werr
		}
		return path, nil
	}
	return "", fmt.Errorf("%w: no free name", ErrSaveRefused)
}
