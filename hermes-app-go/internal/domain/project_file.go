package domain

import (
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// MaxProjectFileBytes is the per-file upload limit (Fase PA, chosen by the owner).
const MaxProjectFileBytes = 100 << 20

const (
	FileKindPDF   = "pdf"
	FileKindImage = "image"
	FileKindText  = "text"
	FileKindOther = "other"
)

// ProjectFile is a file stored (sealed) for one project. Salt/NoncePrefix/ChunkSize are the
// non-secret parameters the filestore needs to decrypt it; the key itself is derived from the
// master key and never stored.
type ProjectFile struct {
	ID          uuid.UUID
	ProjectID   ProjectID
	Name        string
	ContentType string
	Kind        string
	SizeBytes   int64
	SHA256      []byte
	Salt        []byte
	NoncePrefix []byte
	ChunkSize   int
	UploadedBy  string
	CreatedAt   time.Time
	DeletedAt   *time.Time
}

// Inline reports whether the browser may render this file in place. Only types that cannot run
// script are ever served inline; everything else (HTML, SVG, archives…) is a download.
func (f ProjectFile) Inline() bool { return f.Kind != FileKindOther }

// SanitizeFileName keeps only the base name, drops control characters and caps the length.
func SanitizeFileName(raw string) string {
	name := filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		name = "arquivo"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	return name
}

var textExtensions = map[string]bool{".txt": true, ".md": true, ".markdown": true, ".csv": true, ".json": true, ".log": true, ".yaml": true, ".yml": true}

// ClassifyFile decides content type and kind from the sniffed type of the first bytes plus the
// file extension — never from what the client claims. Anything the sniffer sees as HTML, and any
// SVG, is forced to an opaque download.
func ClassifyFile(name, sniffed string) (contentType, kind string) {
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSpace(strings.SplitN(sniffed, ";", 2)[0])
	switch {
	case base == "application/pdf":
		return "application/pdf", FileKindPDF
	case base == "image/png" || base == "image/jpeg" || base == "image/gif" || base == "image/webp":
		return base, FileKindImage
	case base == "text/plain" && ext != ".svg" && ext != ".html" && ext != ".htm" && (textExtensions[ext] || ext == ""):
		return "text/plain; charset=utf-8", FileKindText
	default:
		return "application/octet-stream", FileKindOther
	}
}
