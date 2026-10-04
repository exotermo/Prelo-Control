package domain

import "testing"

func TestClassifyFile(t *testing.T) {
	cases := []struct{ name, sniffed, wantType, wantKind string }{
		{"relatorio.pdf", "application/pdf", "application/pdf", FileKindPDF},
		{"foto.jpg", "image/jpeg", "image/jpeg", FileKindImage},
		{"notas.md", "text/plain; charset=utf-8", "text/plain; charset=utf-8", FileKindText},
		{"pagina.html", "text/html; charset=utf-8", "application/octet-stream", FileKindOther},
		{"falso.pdf", "text/html; charset=utf-8", "application/octet-stream", FileKindOther},
		{"logo.svg", "text/plain; charset=utf-8", "application/octet-stream", FileKindOther},
		{"script.js", "text/plain; charset=utf-8", "application/octet-stream", FileKindOther},
		{"app.zip", "application/zip", "application/octet-stream", FileKindOther},
	}
	for _, c := range cases {
		gotType, gotKind := ClassifyFile(c.name, c.sniffed)
		if gotType != c.wantType || gotKind != c.wantKind {
			t.Errorf("%s (%s): got %s/%s want %s/%s", c.name, c.sniffed, gotType, gotKind, c.wantType, c.wantKind)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	for raw, want := range map[string]string{
		"../../etc/passwd": "passwd",
		"C:\\temp\\x.pdf":  "x.pdf",
		"  ok.txt ":        "ok.txt",
		"a\"b\nc.txt":      "abc.txt",
		"":                 "arquivo",
	} {
		if got := SanitizeFileName(raw); got != want {
			t.Errorf("%q: got %q want %q", raw, got, want)
		}
	}
}
