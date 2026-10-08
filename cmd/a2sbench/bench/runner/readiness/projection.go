package readiness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Fact struct {
	CanonicalPath  string `json:"canonical_path"`
	SourceDocument string `json:"source_document"`
	SourcePath     string `json:"source_path"`
	OKFDocument    string `json:"okf_document"`
	OKFPath        string `json:"okf_path"`
	CanonicalJSON  string `json:"canonical_json"`
	Digest         string `json:"digest"`
	Semantic       bool   `json:"semantic"`
}

type Ledger struct {
	SchemaVersion     string `json:"schema_version"`
	ProjectionVersion string `json:"projection_version"`
	OKFRevision       string `json:"okf_revision"`
	Facts             []Fact `json:"facts"`
	CanonicalDigest   string `json:"canonical_digest"`
	Digest            string `json:"digest"`
}

type Projection struct {
	Project string
	Files   map[string][]byte
	Ledger  Ledger
}

func (p *Projection) Validate() error {
	if p == nil || strings.TrimSpace(p.Project) == "" || len(p.Files) == 0 || len(p.Ledger.Facts) == 0 {
		return fmt.Errorf("projection is incomplete")
	}
	if p.Ledger.SchemaVersion != "okf-source-fact-ledger-v1" || p.Ledger.ProjectionVersion != CatalogProjectionVersion || p.Ledger.OKFRevision != OKFRevision {
		return fmt.Errorf("fact ledger identity differs from the frozen catalog projection")
	}
	if got := canonicalFactDigest(p.Ledger.Facts); got != p.Ledger.CanonicalDigest {
		return fmt.Errorf("canonical fact digest %q, want %q", p.Ledger.CanonicalDigest, got)
	}
	if got := digestJSON(p.Ledger.Facts); got != p.Ledger.Digest {
		return fmt.Errorf("fact ledger digest %q, want %q", p.Ledger.Digest, got)
	}
	seen := map[string]struct{}{}
	for _, fact := range p.Ledger.Facts {
		if fact.Semantic {
			return fmt.Errorf("catalog projection fact %q is incorrectly marked semantic", fact.CanonicalPath)
		}
		if _, duplicate := seen[fact.CanonicalPath]; duplicate {
			return fmt.Errorf("duplicate catalog fact %q", fact.CanonicalPath)
		}
		seen[fact.CanonicalPath] = struct{}{}
		if _, ok := p.Files[fact.OKFDocument]; !ok {
			return fmt.Errorf("fact %q maps to missing OKF document %q", fact.CanonicalPath, fact.OKFDocument)
		}
		if fact.Digest != digestString(fact.CanonicalPath+"\x00"+fact.CanonicalJSON) {
			return fmt.Errorf("fact %q digest is invalid", fact.CanonicalPath)
		}
	}
	for path, body := range p.Files {
		if !strings.HasSuffix(path, ".md") {
			return fmt.Errorf("OKF bundle contains non-Markdown asset %q", path)
		}
		if filepath.Base(path) == "index.md" {
			if path == "index.md" && !bytes.Contains(body, []byte("okf_version: \"0.2\"")) {
				return fmt.Errorf("root OKF index does not record v0.2")
			}
			continue
		}
		frontmatter, err := decodeFrontmatter(body)
		if err != nil {
			return fmt.Errorf("OKF concept %q: %w", path, err)
		}
		kind, _ := frontmatter["type"].(string)
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf("OKF concept %q has no type", path)
		}
		if kind == "Attested Computation" {
			return fmt.Errorf("OKF projection must not contain Attested Computation concepts")
		}
	}
	return validateBundleLinks(p.Files)
}

func decodeFrontmatter(body []byte) (map[string]any, error) {
	if !bytes.HasPrefix(body, []byte("---\n")) {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	end := bytes.Index(body[4:], []byte("\n---\n"))
	if end < 0 {
		return nil, fmt.Errorf("unterminated YAML frontmatter")
	}
	var value map[string]any
	if err := yaml.Unmarshal(body[4:4+end], &value); err != nil {
		return nil, fmt.Errorf("decode YAML frontmatter: %w", err)
	}
	return value, nil
}

func validateBundleLinks(files map[string][]byte) error {
	for source, body := range files {
		remaining := string(body)
		for {
			open := strings.Index(remaining, "](")
			if open < 0 {
				break
			}
			remaining = remaining[open+2:]
			close := strings.IndexByte(remaining, ')')
			if close < 0 {
				return fmt.Errorf("OKF asset %q contains an unterminated Markdown link", source)
			}
			link := strings.TrimSpace(remaining[:close])
			remaining = remaining[close+1:]
			if link == "" || strings.Contains(link, "://") || strings.HasPrefix(link, "#") {
				continue
			}
			link = strings.SplitN(link, "#", 2)[0]
			directoryLink := strings.HasSuffix(link, "/")
			var target string
			if strings.HasPrefix(link, "/") {
				target = strings.TrimPrefix(link, "/")
			} else {
				target = filepath.ToSlash(filepath.Join(filepath.Dir(source), filepath.FromSlash(link)))
			}
			if directoryLink {
				target = filepath.ToSlash(filepath.Join(filepath.FromSlash(target), "index.md"))
			}
			target = filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
			if _, ok := files[target]; !ok {
				return fmt.Errorf("OKF asset %q links to missing bundle path %q", source, target)
			}
		}
	}
	return nil
}

func (p *Projection) WriteBundle(root string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	for path, body := range p.Files {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		// A projection may be written once as the read-only source-adapter
		// snapshot and again after a reference-agent pass enriches it. Make an
		// existing asset writable only for this owned replacement, then restore
		// the immutable on-disk mode below.
		if err := os.Chmod(target, 0o600); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prepare OKF asset %q for replacement: %w", path, err)
		}
		if err := os.WriteFile(target, body, 0o400); err != nil {
			return fmt.Errorf("write OKF asset %q: %w", path, err)
		}
		if err := os.Chmod(target, 0o400); err != nil {
			return fmt.Errorf("freeze OKF asset %q: %w", path, err)
		}
	}
	return nil
}

func TreeDigest(root string) (string, error) {
	hash := sha256.New()
	var paths []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, path := range paths {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(body)
		hash.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func escapeMarkdownTable(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '-', char == '_':
			out.WriteRune(char)
		default:
			out.WriteByte('_')
		}
	}
	if out.Len() == 0 {
		return "unnamed"
	}
	return out.String()
}

func yamlString(value string) string { body, _ := json.Marshal(value); return string(body) }
func yamlStringList(values []string) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = yamlString(value)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
func digestJSON(value any) string { body, _ := json.Marshal(value); return digestString(string(body)) }
func canonicalFactDigest(facts []Fact) string {
	type canonicalFact struct {
		Path     string `json:"path"`
		Value    string `json:"value"`
		Semantic bool   `json:"semantic"`
	}
	canonical := make([]canonicalFact, len(facts))
	for index, fact := range facts {
		canonical[index] = canonicalFact{Path: fact.CanonicalPath, Value: fact.CanonicalJSON, Semantic: fact.Semantic}
	}
	return digestJSON(canonical)
}
