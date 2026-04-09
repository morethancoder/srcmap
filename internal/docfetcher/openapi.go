package docfetcher

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPIParser extracts documentation from OpenAPI/Swagger specs.
type OpenAPIParser struct{}

type openAPISpec struct {
	Info struct {
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
		Version     string `yaml:"version"`
	} `yaml:"info"`
	Paths map[string]map[string]openAPIOperation `yaml:"paths"`
}

type openAPIOperation struct {
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
	OperationID string `yaml:"operationId"`
	Tags        []string `yaml:"tags"`
	Parameters  []openAPIParameter `yaml:"parameters"`
	Responses   map[string]openAPIResponse `yaml:"responses"`
}

type openAPIParameter struct {
	Name        string `yaml:"name"`
	In          string `yaml:"in"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

type openAPIResponse struct {
	Description string `yaml:"description"`
}

// Parse parses an OpenAPI spec and returns one RawPage per operation.
func (p *OpenAPIParser) Parse(content []byte) ([]RawPage, error) {
	var spec openAPISpec
	if err := yaml.Unmarshal(content, &spec); err != nil {
		return nil, fmt.Errorf("parsing OpenAPI spec: %w", err)
	}

	var pages []RawPage

	// Sort paths for deterministic output
	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		methods := spec.Paths[path]
		// Sort methods
		methodNames := make([]string, 0, len(methods))
		for method := range methods {
			methodNames = append(methodNames, method)
		}
		sort.Strings(methodNames)

		for _, method := range methodNames {
			op := methods[method]
			text := formatOperation(method, path, op)

			h := sha256.Sum256([]byte(text))
			pages = append(pages, RawPage{
				URL:         fmt.Sprintf("%s %s", strings.ToUpper(method), path),
				Title:       op.Summary,
				Content:     text,
				Fingerprint: fmt.Sprintf("%x", h),
			})
		}
	}

	return pages, nil
}

func formatOperation(method, path string, op openAPIOperation) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## %s %s\n\n", strings.ToUpper(method), path))

	if op.Summary != "" {
		b.WriteString(op.Summary + "\n\n")
	}
	if op.Description != "" {
		b.WriteString(op.Description + "\n\n")
	}
	if op.OperationID != "" {
		b.WriteString(fmt.Sprintf("**Operation ID:** %s\n\n", op.OperationID))
	}

	if len(op.Parameters) > 0 {
		b.WriteString("**Parameters:**\n")
		for _, param := range op.Parameters {
			required := ""
			if param.Required {
				required = " (required)"
			}
			b.WriteString(fmt.Sprintf("- `%s` (%s)%s: %s\n", param.Name, param.In, required, param.Description))
		}
		b.WriteString("\n")
	}

	if len(op.Responses) > 0 {
		b.WriteString("**Responses:**\n")
		codes := make([]string, 0, len(op.Responses))
		for code := range op.Responses {
			codes = append(codes, code)
		}
		sort.Strings(codes)
		for _, code := range codes {
			resp := op.Responses[code]
			b.WriteString(fmt.Sprintf("- %s: %s\n", code, resp.Description))
		}
	}

	return b.String()
}
