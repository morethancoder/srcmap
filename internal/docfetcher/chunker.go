package docfetcher

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxTokens    = 8000
	minTokens    = 1000
	maxBatchSize = 10
	flatProseMax = 4000
	tokenPerWord = 1.3
)

// DocType classifies the structure of a document for chunking.
type DocType string

const (
	DocHeadingStructured DocType = "heading-structured"
	DocOpenAPI           DocType = "openapi"
	DocMarkdown          DocType = "markdown"
	DocFlatProse         DocType = "flat-prose"
	DocAnchorStructured  DocType = "anchor-structured"
)

// DefaultChunker splits raw pages into token-bounded chunks.
type DefaultChunker struct{}

// Chunk splits pages into token-bounded chunks with context headers.
func (c *DefaultChunker) Chunk(sourceName string, pages []RawPage) ([]Chunk, error) {
	return c.ChunkWithOrigin(sourceName, "", pages)
}

// ChunkWithOrigin is like Chunk but includes an [Origin] header when originURL is non-empty.
func (c *DefaultChunker) ChunkWithOrigin(sourceName, originURL string, pages []RawPage) ([]Chunk, error) {
	var allChunks []Chunk
	chunkIdx := 0
	seenFP := map[string]struct{}{}

	for _, page := range pages {
		docType := detectDocType(page.Content)
		var sections []section

		switch docType {
		case DocOpenAPI:
			// Each OpenAPI page is already one operation — pass through
			sections = []section{{heading: page.Title, content: page.Content}}
		case DocAnchorStructured:
			sections = splitByAnchors(page.Content)
		case DocMarkdown:
			sections = splitMarkdown(page.Content)
		case DocHeadingStructured:
			sections = splitByHeadings(page.Content)
		case DocFlatProse:
			sections = splitFlatProse(page.Content)
		}

		// Enforce max token limit — split oversized sections further
		var sized []section
		for _, s := range sections {
			tokens := estimateTokens(s.content)
			if tokens > maxTokens {
				parts := splitLargeSection(s)
				for i := range parts {
					if parts[i].anchorID == "" {
						parts[i].anchorID = s.anchorID
					}
				}
				sized = append(sized, parts...)
			} else {
				sized = append(sized, s)
			}
		}

		// Batch small chunks — but never for anchor-structured docs: we
		// want exactly one chunk per named entity so the linker can map
		// anchor → file cleanly (e.g. one Telegram method per chunk).
		if docType != DocAnchorStructured {
			sized = batchSmallChunks(sized)
		}

		totalChunks := len(sized)
		pageKind := classifyPageKind(page.URL, page.Title)
		for i, s := range sized {
			header := buildContextHeader(sourceName, page.Title, s.heading, originURL, i+1, totalChunks)
			content := header + "\n\n" + s.content

			h := sha256.Sum256([]byte(s.content))
			fp := fmt.Sprintf("%x", h)
			if _, dup := seenFP[fp]; dup {
				continue
			}
			seenFP[fp] = struct{}{}

			kind := pageKind
			if kind == ChunkKindDoc && isChangelogHeading(s.heading) {
				kind = ChunkKindChangelog
			}

			allChunks = append(allChunks, Chunk{
				SourceID:        sourceName,
				PageURL:         page.URL,
				ChunkIndex:      chunkIdx,
				Heading:         s.heading,
				Content:         content,
				EstimatedTokens: estimateTokens(content),
				Fingerprint:     fp,
				Status:          ChunkPending,
				Kind:            kind,
				AnchorID:        s.anchorID,
			})
			chunkIdx++
		}
	}

	return allChunks, nil
}

type section struct {
	heading  string
	content  string
	anchorID string
}

// estimateTokens estimates token count: word_count * 1.3
func estimateTokens(text string) int {
	words := len(strings.Fields(text))
	return int(float64(words) * tokenPerWord)
}

var (
	htmlHeadingRe = regexp.MustCompile(`(?i)<h([2-4])[^>]*>(.*?)</h[2-4]>`)
	mdHeading2Re  = regexp.MustCompile(`(?m)^##\s+(.+)$`)
	mdHeading3Re  = regexp.MustCompile(`(?m)^###\s+(.+)$`)
)

func detectDocType(content string) DocType {
	// Anchor markers injected by the crawler take priority — they carry
	// semantic boundaries we want to preserve (one method = one chunk).
	if strings.Contains(content, AnchorMarker) {
		return DocAnchorStructured
	}

	// Check for markdown headings
	if mdHeading2Re.MatchString(content) || mdHeading3Re.MatchString(content) {
		return DocMarkdown
	}

	// Check for HTML headings
	if htmlHeadingRe.MatchString(content) {
		return DocHeadingStructured
	}

	return DocFlatProse
}

var anchorSplitRe = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(AnchorMarker) + `([^@\n]+)@@\s*$`)

// splitByAnchors splits content at injected `@@SMANCHOR:id@@` markers.
// Each section gets the anchor ID stored so downstream tools can route
// by stable name rather than by heading text alone.
func splitByAnchors(content string) []section {
	locs := anchorSplitRe.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return []section{{content: content}}
	}
	var out []section
	// Preamble before first anchor.
	if locs[0][0] > 0 {
		pre := strings.TrimSpace(content[:locs[0][0]])
		if pre != "" {
			out = append(out, section{heading: "Introduction", content: pre})
		}
	}
	for i, loc := range locs {
		id := content[loc[2]:loc[3]]
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := strings.TrimSpace(content[loc[1]:end])
		heading := extractSectionHeading(body)
		if heading == "" {
			heading = id
		}
		// Strip any residual marker from body.
		body = anchorSplitRe.ReplaceAllString(body, "")
		out = append(out, section{
			heading:  heading,
			content:  strings.TrimSpace(body),
			anchorID: id,
		})
	}
	return out
}

var (
	// Anchor tightly — `history` / `releases` as bare segments occur in
	// plenty of non-changelog URLs (e.g. /api/browser-history,
	// /repos/{o}/{r}/releases). Rely on title heuristics there instead.
	changelogURLRe = regexp.MustCompile(`(?i)(?:^|/)(?:changelog|release-notes|whats-new|whatsnew|releasenotes|news)(?:/|$|[#?])`)
	changelogHRe   = regexp.MustCompile(`(?i)^(changelog|release notes|recent changes|what'?s new|history|releases?\b|version \d)`)
)

func classifyPageKind(pageURL, title string) ChunkKind {
	if changelogURLRe.MatchString(pageURL) {
		return ChunkKindChangelog
	}
	if changelogHRe.MatchString(strings.TrimSpace(title)) {
		return ChunkKindChangelog
	}
	return ChunkKindDoc
}

func isChangelogHeading(h string) bool {
	return changelogHRe.MatchString(strings.TrimSpace(h))
}

func splitMarkdown(content string) []section {
	return splitByPattern(content, `(?m)^##\s+`, `(?m)^###\s+`)
}

func splitByHeadings(content string) []section {
	return splitByPattern(content, `(?i)<h2[^>]*>`, `(?i)<h3[^>]*>`)
}

func splitByPattern(content string, primaryPattern, secondaryPattern string) []section {
	re := regexp.MustCompile(primaryPattern)
	locs := re.FindAllStringIndex(content, -1)

	if len(locs) == 0 {
		// Try secondary pattern
		re = regexp.MustCompile(secondaryPattern)
		locs = re.FindAllStringIndex(content, -1)
	}

	if len(locs) == 0 {
		return []section{{content: content}}
	}

	var sections []section

	// Content before first heading
	if locs[0][0] > 0 {
		preamble := strings.TrimSpace(content[:locs[0][0]])
		if preamble != "" {
			sections = append(sections, section{heading: "Introduction", content: preamble})
		}
	}

	for i, loc := range locs {
		end := len(content)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}

		chunk := content[loc[0]:end]
		heading := extractSectionHeading(chunk)
		sections = append(sections, section{heading: heading, content: strings.TrimSpace(chunk)})
	}

	return sections
}

func splitFlatProse(content string) []section {
	paragraphs := strings.Split(content, "\n\n")
	var sections []section
	var current strings.Builder
	currentTokens := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		paraTokens := estimateTokens(para)

		if currentTokens+paraTokens > flatProseMax && currentTokens > 0 {
			sections = append(sections, section{content: current.String()})
			current.Reset()
			currentTokens = 0
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
		currentTokens += paraTokens
	}

	if current.Len() > 0 {
		sections = append(sections, section{content: current.String()})
	}

	return sections
}

func splitLargeSection(s section) []section {
	// Try splitting at sub-headings (### for markdown)
	subsections := splitByPattern(s.content, `(?m)^###\s+`, `(?m)^####\s+`)
	if len(subsections) > 1 {
		// Check if any are still too large
		var result []section
		for _, sub := range subsections {
			if estimateTokens(sub.content) > maxTokens {
				result = append(result, splitFlatProse(sub.content)...)
			} else {
				result = append(result, sub)
			}
		}
		return result
	}

	// Fall back to paragraph splitting
	return splitFlatProse(s.content)
}

func batchSmallChunks(sections []section) []section {
	var result []section
	var batch []section
	batchTokens := 0

	for _, s := range sections {
		tokens := estimateTokens(s.content)

		if tokens >= minTokens {
			// Flush any pending batch
			if len(batch) > 0 {
				result = append(result, mergeBatch(batch))
				batch = nil
				batchTokens = 0
			}
			result = append(result, s)
			continue
		}

		batch = append(batch, s)
		batchTokens += tokens

		if len(batch) >= maxBatchSize || batchTokens >= minTokens {
			result = append(result, mergeBatch(batch))
			batch = nil
			batchTokens = 0
		}
	}

	if len(batch) > 0 {
		result = append(result, mergeBatch(batch))
	}

	return result
}

func mergeBatch(batch []section) section {
	var headings []string
	var contents []string
	for _, s := range batch {
		if s.heading != "" {
			headings = append(headings, s.heading)
		}
		contents = append(contents, s.content)
	}
	heading := strings.Join(headings, " / ")
	return section{heading: heading, content: strings.Join(contents, "\n\n")}
}

func buildContextHeader(sourceName, pageTitle, sectionHeading, originURL string, chunkNum, totalChunks int) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("[Source: %s]", sourceName))

	if originURL != "" {
		lines = append(lines, fmt.Sprintf("[Origin: %s]", originURL))
	}

	if pageTitle != "" {
		lines = append(lines, fmt.Sprintf("[Section: %s]", pageTitle))
	}

	if sectionHeading != "" && sectionHeading != pageTitle {
		breadcrumb := sourceName
		if pageTitle != "" {
			breadcrumb += " → " + pageTitle
		}
		breadcrumb += " → " + sectionHeading
		lines = append(lines, fmt.Sprintf("[Breadcrumb: %s]", breadcrumb))
	}

	lines = append(lines, fmt.Sprintf("[Chunk %d of %d]", chunkNum, totalChunks))

	return strings.Join(lines, "\n")
}

func extractSectionHeading(text string) string {
	// Try markdown heading
	if m := regexp.MustCompile(`(?m)^#{1,4}\s+(.+)$`).FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	// Try HTML heading
	if m := regexp.MustCompile(`(?i)<h[2-4][^>]*>(.*?)</h[2-4]>`).FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	// Return first line
	if idx := strings.IndexByte(text, '\n'); idx > 0 {
		return strings.TrimSpace(text[:idx])
	}
	return ""
}
