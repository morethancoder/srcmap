package docfetcher

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MarkdownWalker extracts documentation from a local markdown directory.
type MarkdownWalker struct{}

// Walk reads all .md and .mdx files from a directory and returns RawPages.
func (w *MarkdownWalker) Walk(dir string) ([]RawPage, error) {
	var pages []RawPage

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == "node_modules" || name == ".git" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".mdx" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(dir, path)
		title := extractMarkdownTitle(string(content))
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(path), ext)
		}

		h := sha256.Sum256(content)
		pages = append(pages, RawPage{
			URL:         relPath,
			Title:       title,
			Content:     string(content),
			Fingerprint: fmt.Sprintf("%x", h),
		})

		return nil
	})

	return pages, err
}

func extractMarkdownTitle(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}
