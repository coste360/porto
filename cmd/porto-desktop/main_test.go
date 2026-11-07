package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLI_DesktopVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-version"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if got := out.String(); !strings.HasPrefix(got, "porto-desktop ") || !strings.HasSuffix(got, "(browser)\n") {
		t.Fatalf("output %q", got)
	}
	if code := run([]string{"-bogus"}, &out, &errb); code != 2 {
		t.Fatalf("usage exit %d", code)
	}
}
