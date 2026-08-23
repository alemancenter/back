// Package fileextract reads plain text out of uploaded article/post attachments (DOCX, PDF,
// and plain text-ish formats) so AI content pipelines can use real file content as source
// material. Extracted here — not in internal/services/contentaudit — because both plain
// internal/services and internal/services/contentaudit need it, and contentaudit already
// imports internal/services (as coreai), so the reverse import would cycle. Mirrors the
// internal/contentquality precedent: a neutral package depending only on stdlib + models,
// safe for both sides to import.
package fileextract

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/imanjo/fiber-api/internal/models"
)

const (
	// ReadLimit caps how many bytes are read from a plain-text-ish file or piped out of
	// pdftotext — a safety ceiling against pathologically large uploads, not a content policy.
	ReadLimit = int64(64 * 1024)
	// DOCXXMLLimit caps how many bytes of word/document.xml are decoded — DOCX files are
	// zip-compressed, so this guards against a small file expanding into a huge XML stream.
	DOCXXMLLimit = int64(16 * 1024 * 1024)
	// MaxTextRunes caps the plain text this package will ever return for one file. Callers
	// that need a smaller evidence-pack-specific limit truncate further on their own.
	MaxTextRunes = 20000
)

var (
	scriptStylePattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>`)
	htmlTagPattern     = regexp.MustCompile(`(?is)<[^>]+>`)
	whitespacePattern  = regexp.MustCompile(`\s+`)
)

func normalizePlainText(raw string) string {
	text := scriptStylePattern.ReplaceAllString(raw, " ")
	text = htmlTagPattern.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(text, " "))
}

// ResolveAttachmentPath turns a File.FilePath (stored relative, e.g. "files/posts/ab12cd.pdf")
// into a real, existing absolute path on disk. storageRoot should be config.Get().Storage.Path
// — the same value internal/services/file_service.go itself writes uploads under — checked
// first. IMANJO_STORAGE_ROOT/STORAGE_ROOT env vars remain as a fallback for any deployment that
// sets them explicitly, but are no longer the primary (and only, previously mismatched) source
// of truth.
func ResolveAttachmentPath(raw, storageRoot string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return "", false
	}
	clean := filepath.Clean(raw)
	candidates := []string{}
	if filepath.IsAbs(clean) {
		candidates = append(candidates, clean)
	}
	trimmed := strings.TrimLeft(clean, "/\\")
	roots := []string{
		strings.TrimSpace(storageRoot),
		strings.TrimSpace(os.Getenv("IMANJO_STORAGE_ROOT")),
		strings.TrimSpace(os.Getenv("STORAGE_ROOT")),
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		candidates = append(candidates, filepath.Join(root, strings.TrimPrefix(trimmed, "storage/")))
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// ReadAttachmentEvidence extracts plain text from an uploaded file for use as AI source
// material. storageRoot should be config.Get().Storage.Path (see ResolveAttachmentPath).
func ReadAttachmentEvidence(file models.File, storageRoot string) (string, bool) {
	path, ok := ResolveAttachmentPath(file.FilePath, storageRoot)
	if !ok {
		return "", false
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".txt", ".md", ".csv", ".json", ".html", ".htm", ".xml":
		f, err := os.Open(path)
		if err != nil {
			return "", false
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, ReadLimit))
		if err != nil {
			return "", false
		}
		text := string(data)
		if ext == ".html" || ext == ".htm" || ext == ".xml" {
			text = normalizePlainText(text)
		}
		text = strings.TrimSpace(text)
		return text, text != ""
	case ".docx":
		text, err := ExtractDOCXText(path)
		return text, err == nil && strings.TrimSpace(text) != ""
	case ".pdf":
		text, err := ExtractPDFText(path)
		return text, err == nil && strings.TrimSpace(text) != ""
	default:
		return "", false
	}
}

// ExtractDOCXText pulls plain text out of word/document.xml inside a .docx (zip) file.
func ExtractDOCXText(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		if f.UncompressedSize64 > uint64(DOCXXMLLimit) {
			return "", fmt.Errorf("docx document.xml exceeds safety limit: %d bytes", f.UncompressedSize64)
		}
		r, err := f.Open()
		if err != nil {
			return "", err
		}

		decoder := xml.NewDecoder(io.LimitReader(r, DOCXXMLLimit))
		var out strings.Builder
		inText := false
		runeCount := 0

		appendText := func(value string) {
			for _, ch := range value {
				if runeCount >= MaxTextRunes {
					return
				}
				out.WriteRune(ch)
				runeCount++
			}
		}
		appendSeparator := func(ch rune) {
			if runeCount >= MaxTextRunes || out.Len() == 0 {
				return
			}
			out.WriteRune(ch)
			runeCount++
		}

		for {
			token, tokenErr := decoder.Token()
			if tokenErr == io.EOF {
				break
			}
			if tokenErr != nil {
				r.Close()
				return "", tokenErr
			}

			switch node := token.(type) {
			case xml.StartElement:
				switch node.Name.Local {
				case "t":
					inText = true
				case "tab":
					appendSeparator(' ')
				case "br":
					appendSeparator('\n')
				}
			case xml.CharData:
				if inText {
					appendText(string(node))
				}
			case xml.EndElement:
				if node.Name.Local == "t" {
					inText = false
				}
				if node.Name.Local == "p" {
					appendSeparator('\n')
				}
			}
			if runeCount >= MaxTextRunes {
				break
			}
		}
		r.Close()

		text := strings.TrimSpace(out.String())
		if text == "" {
			return "", errors.New("docx document.xml contains no readable text")
		}
		return text, nil
	}
	return "", errors.New("docx document.xml not found")
}

// ExtractPDFText shells out to pdftotext (poppler-utils) — must be installed on the host.
func ExtractPDFText(path string) (string, error) {
	binary, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-layout", path, "-")
	var out strings.Builder
	cmd.Stdout = &limitedWriter{W: &out, N: ReadLimit}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}

type limitedWriter struct {
	W io.Writer
	N int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.N <= 0 {
		return len(p), nil
	}
	toWrite := p
	if int64(len(toWrite)) > w.N {
		toWrite = toWrite[:w.N]
	}
	n, err := w.W.Write(toWrite)
	w.N -= int64(n)
	if err != nil {
		return n, err
	}
	return len(p), nil
}
