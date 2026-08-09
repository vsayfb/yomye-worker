package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vsayfb/gig-platform-categorization-service/internal/extractor"
)

const BaselineVersion = 1

type Baseline struct {
	Version       int       `json:"version"`
	CapturedAt    time.Time `json:"captured_at"`
	CatalogSHA256 string    `json:"catalog_sha256,omitempty"`
	Cases         []Case    `json:"cases"`
}

type Case struct {
	Description  string         `json:"description"`
	SourceLocale string         `json:"source_locale"`
	CategoryPath []CategoryNode `json:"category_path"`
	GeneratedBy  string         `json:"generated_by,omitempty"`
}

type CategoryNode struct {
	CanonicalSlug string `json:"canonical_slug"`
	IsSelectable  bool   `json:"is_selectable"`
}

type Comparison struct {
	Exact          bool
	LocaleMatch    bool
	PathMatch      bool
	LeafMatch      bool
	ExpectedPath   string
	ActualPath     string
	ExpectedLocale string
	ActualLocale   string
}

func CaseFromExtraction(description string, result *extractor.ExtractedListing) (Case, error) {
	if result == nil {
		return Case{}, fmt.Errorf("nil extraction")
	}
	if strings.TrimSpace(result.SourceLocale) == "" {
		return Case{}, fmt.Errorf("missing source locale")
	}
	if len(result.CategoryPath) == 0 {
		return Case{}, fmt.Errorf("empty category path")
	}

	path := make([]CategoryNode, 0, len(result.CategoryPath))
	for index, node := range result.CategoryPath {
		slug := strings.TrimSpace(node.CanonicalSlug)
		if slug == "" {
			return Case{}, fmt.Errorf("category path node %d has empty canonical slug", index)
		}
		path = append(path, CategoryNode{
			CanonicalSlug: slug,
			IsSelectable:  node.IsSelectable,
		})
	}

	return Case{
		Description:  strings.TrimSpace(description),
		SourceLocale: strings.TrimSpace(result.SourceLocale),
		CategoryPath: path,
		GeneratedBy:  result.GeneratedBy,
	}, nil
}

func Compare(expected Case, actual *extractor.ExtractedListing) (Comparison, error) {
	actualCase, err := CaseFromExtraction(expected.Description, actual)
	if err != nil {
		return Comparison{}, err
	}

	comparison := Comparison{
		LocaleMatch:    expected.SourceLocale == actualCase.SourceLocale,
		ExpectedPath:   formatPath(expected.CategoryPath),
		ActualPath:     formatPath(actualCase.CategoryPath),
		ExpectedLocale: expected.SourceLocale,
		ActualLocale:   actualCase.SourceLocale,
	}
	comparison.PathMatch = pathsEqual(expected.CategoryPath, actualCase.CategoryPath)
	comparison.LeafMatch = leafEqual(expected.CategoryPath, actualCase.CategoryPath)
	comparison.Exact = comparison.LocaleMatch && comparison.PathMatch

	return comparison, nil
}

func Load(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}

	var baseline Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("decode baseline: %w", err)
	}
	if err := baseline.Validate(); err != nil {
		return nil, err
	}
	return &baseline, nil
}

func Write(path string, baseline Baseline, overwrite bool) error {
	if err := baseline.Validate(); err != nil {
		return err
	}

	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("refusing to overwrite existing baseline %q without -force", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat baseline output: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create baseline directory: %w", err)
	}

	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

func (b Baseline) Validate() error {
	if b.Version != BaselineVersion {
		return fmt.Errorf("unsupported evaluation baseline version %d", b.Version)
	}
	if len(b.Cases) == 0 {
		return fmt.Errorf("evaluation baseline has no cases")
	}

	seen := make(map[string]struct{}, len(b.Cases))
	for index, item := range b.Cases {
		description := strings.TrimSpace(item.Description)
		if description == "" {
			return fmt.Errorf("case %d has empty description", index+1)
		}
		if _, exists := seen[description]; exists {
			return fmt.Errorf("case %d duplicates description %q", index+1, description)
		}
		seen[description] = struct{}{}

		if strings.TrimSpace(item.SourceLocale) == "" {
			return fmt.Errorf("case %d has empty source locale", index+1)
		}
		if len(item.CategoryPath) == 0 {
			return fmt.Errorf("case %d has empty category path", index+1)
		}
		for nodeIndex, node := range item.CategoryPath {
			if strings.TrimSpace(node.CanonicalSlug) == "" {
				return fmt.Errorf("case %d category node %d has empty canonical slug", index+1, nodeIndex+1)
			}
		}
	}
	return nil
}

func pathsEqual(expected, actual []CategoryNode) bool {
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return false
		}
	}
	return true
}

func leafEqual(expected, actual []CategoryNode) bool {
	if len(expected) == 0 || len(actual) == 0 {
		return false
	}
	return expected[len(expected)-1] == actual[len(actual)-1]
}

func formatPath(path []CategoryNode) string {
	parts := make([]string, 0, len(path))
	for _, node := range path {
		marker := ""
		if node.IsSelectable {
			marker = "*"
		}
		parts = append(parts, node.CanonicalSlug+marker)
	}
	return strings.Join(parts, " > ")
}
