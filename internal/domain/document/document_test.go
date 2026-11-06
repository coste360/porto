package document

import "testing"

func TestSanitizeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"polizza.pdf":                 "polizza.pdf",
		"../../etc/passwd":            "passwd",
		`C:\Users\mario\polizza.pdf`:  "polizza.pdf",
		"bad\x00name\n.pdf":           "badname.pdf",
		"  ":                          "",
		"/":                           "",
		"a<b>c:d*e?f\"g|h.png":        "abcdefgh.png",
		string(make([]byte, 0)) + ".": "",
	} {
		if got := SanitizeFileName(in); got != want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := ""
	for range 200 {
		long += "x"
	}
	if got := SanitizeFileName(long + ".pdf"); len([]rune(got)) != maxFileName {
		t.Errorf("long name kept %d runes", len([]rune(got)))
	}
}
