package repository

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/drimini-software/release-evidence/internal/model"
)

const (
	defaultMaxEntries        = 25_000
	defaultMaxDepth          = 32
	defaultMaxFileBytes      = 1 << 20
	defaultMaxTotalReadBytes = 8 << 20
	maxGitignoreBytes        = 64 << 10
	maxGitMetadataBytes      = 1 << 20
)

var ignoredDirectoryNames = map[string]struct{}{
	".git":              {},
	".astro":            {},
	".cache":            {},
	".next":             {},
	".nuxt":             {},
	".turbo":            {},
	".wrangler":         {},
	"build":             {},
	"coverage":          {},
	"dist":              {},
	"node_modules":      {},
	"playwright-report": {},
	"test-results":      {},
}

var defaultExclusions = []string{
	"built-in: VCS metadata (.git/)",
	"built-in: dependencies and generated output",
	"built-in: tool caches and test artifacts",
	"built-in: local environment and secret files",
	"built-in: log files",
}

type Options struct {
	MaxEntries        int
	MaxDepth          int
	MaxFileBytes      int64
	MaxTotalReadBytes int64
}

func DefaultOptions() Options {
	return Options{
		MaxEntries:        defaultMaxEntries,
		MaxDepth:          defaultMaxDepth,
		MaxFileBytes:      defaultMaxFileBytes,
		MaxTotalReadBytes: defaultMaxTotalReadBytes,
	}
}

type Snapshot struct {
	Root             string
	Name             string
	Paths            []string
	Exclusions       []string
	Diagnostics      []model.Diagnostic
	Complete         bool
	EntriesVisited   int
	Revision         string
	RevisionState    model.State
	WorkingTreeState model.State
	WorkingTreeNote  string
	Limits           model.Limits
}

type ignoreRule struct {
	raw      string
	pattern  string
	negate   bool
	anchored bool
	dirOnly  bool
}

func Inspect(ctx context.Context, root string, options Options) (Snapshot, error) {
	options = withDefaults(options)

	absolute, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository links: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect repository root: %w", err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("repository root is not a directory")
	}

	rules, ruleExclusions, diagnostics := loadIgnoreRules(resolved)
	revision, revisionState, revisionDiagnostics := readRevision(resolved)
	diagnostics = append(diagnostics, revisionDiagnostics...)

	snapshot := Snapshot{
		Root:             resolved,
		Name:             filepath.Base(resolved),
		Exclusions:       append(append([]string{}, defaultExclusions...), ruleExclusions...),
		Diagnostics:      diagnostics,
		Complete:         true,
		Revision:         revision,
		RevisionState:    revisionState,
		WorkingTreeState: model.StateUnknown,
		WorkingTreeNote:  "Working-tree state is not inspected in the first slice; no repository command is executed.",
		Limits: model.Limits{
			MaxEntries:        options.MaxEntries,
			MaxDepth:          options.MaxDepth,
			MaxFileBytes:      options.MaxFileBytes,
			MaxTotalReadBytes: options.MaxTotalReadBytes,
		},
	}

	err = filepath.WalkDir(resolved, func(current string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			snapshot.Complete = false
			return fs.SkipAll
		}

		relative, relErr := filepath.Rel(resolved, current)
		if relErr != nil {
			snapshot.Complete = false
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "path_relative_failed",
				Severity: "warning",
				Message:  "A repository path could not be normalized and was skipped.",
			})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "." {
			if walkErr != nil {
				return walkErr
			}
			return nil
		}
		relative = filepath.ToSlash(relative)

		snapshot.EntriesVisited++
		if snapshot.EntriesVisited > options.MaxEntries {
			snapshot.Complete = false
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "entry_budget_exceeded",
				Severity: "warning",
				Message:  "The scan stopped after reaching the configured entry budget.",
			})
			return fs.SkipAll
		}

		if walkErr != nil {
			snapshot.Complete = false
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "entry_unreadable",
				Severity: "warning",
				Path:     relative,
				Message:  "A repository entry could not be inspected.",
			})
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		depth := strings.Count(relative, "/") + 1
		if depth > options.MaxDepth {
			snapshot.Complete = false
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "depth_budget_exceeded",
				Severity: "warning",
				Path:     relative,
				Message:  "A repository path exceeded the configured depth budget and was skipped.",
			})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		linkLike, linkErr := isLinkEntry(entry)
		if linkErr != nil {
			snapshot.Complete = false
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "link_metadata_unreadable",
				Severity: "warning",
				Path:     relative,
				Message:  "Link metadata could not be inspected, so the entry was skipped.",
			})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if linkLike {
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "link_skipped",
				Severity: "info",
				Path:     relative,
				Message:  "A symbolic link was not followed.",
			})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if isDefaultIgnored(relative, entry.IsDir()) || ignoredByRules(relative, entry.IsDir(), rules) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if entry.IsDir() {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			snapshot.Complete = false
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "file_metadata_unreadable",
				Severity: "warning",
				Path:     relative,
				Message:  "File metadata could not be inspected.",
			})
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			snapshot.Diagnostics = append(snapshot.Diagnostics, model.Diagnostic{
				Code:     "special_file_skipped",
				Severity: "info",
				Path:     relative,
				Message:  "A non-regular file was skipped.",
			})
			return nil
		}

		snapshot.Paths = append(snapshot.Paths, relative)
		return nil
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("walk repository: %w", err)
	}

	sort.Strings(snapshot.Paths)
	sort.Slice(snapshot.Diagnostics, func(i, j int) bool {
		left := snapshot.Diagnostics[i].Code + "\x00" + snapshot.Diagnostics[i].Path
		right := snapshot.Diagnostics[j].Code + "\x00" + snapshot.Diagnostics[j].Path
		return left < right
	})
	return snapshot, nil
}

func withDefaults(options Options) Options {
	defaults := DefaultOptions()
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaults.MaxEntries
	}
	if options.MaxDepth <= 0 {
		options.MaxDepth = defaults.MaxDepth
	}
	if options.MaxFileBytes <= 0 {
		options.MaxFileBytes = defaults.MaxFileBytes
	}
	if options.MaxTotalReadBytes <= 0 {
		options.MaxTotalReadBytes = defaults.MaxTotalReadBytes
	}
	return options
}

func isDefaultIgnored(relative string, isDir bool) bool {
	segments := strings.Split(relative, "/")
	for _, segment := range segments {
		if _, ignored := ignoredDirectoryNames[segment]; ignored {
			return true
		}
	}

	base := strings.ToLower(path.Base(relative))
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".dev.vars" || strings.HasPrefix(base, ".dev.vars.") {
		return true
	}
	if !isDir && strings.HasSuffix(base, ".log") {
		return true
	}
	return false
}

func loadIgnoreRules(root string) ([]ignoreRule, []string, []model.Diagnostic) {
	content, err := readBoundedFileUnder(root, ".gitignore", maxGitignoreBytes)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, []model.Diagnostic{{
			Code:     "gitignore_unreadable",
			Severity: "warning",
			Path:     ".gitignore",
			Message:  "The root .gitignore could not be read within the supported size and file-type boundary.",
		}}
	}

	var rules []ignoreRule
	var exclusions []string
	var diagnostics []model.Diagnostic
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "\\#") || strings.HasPrefix(line, "\\!") {
			line = line[1:]
		}

		rule := ignoreRule{raw: line}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = strings.TrimPrefix(line, "!")
		}
		if strings.HasPrefix(line, "/") {
			rule.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if line == "" {
			continue
		}
		if _, err := path.Match(line, "probe"); err != nil {
			diagnostics = append(diagnostics, model.Diagnostic{
				Code:     "gitignore_pattern_unsupported",
				Severity: "warning",
				Path:     ".gitignore",
				Message:  "An invalid ignore pattern was skipped.",
			})
			continue
		}
		rule.pattern = line
		rules = append(rules, rule)
		exclusions = append(exclusions, ".gitignore: "+rule.raw)
	}
	if err := scanner.Err(); err != nil {
		diagnostics = append(diagnostics, model.Diagnostic{
			Code:     "gitignore_parse_failed",
			Severity: "warning",
			Path:     ".gitignore",
			Message:  "The root .gitignore could not be parsed completely.",
		})
	}
	return rules, exclusions, diagnostics
}

func ignoredByRules(relative string, isDir bool, rules []ignoreRule) bool {
	ignored := false
	for _, rule := range rules {
		if matchesIgnoreRule(relative, isDir, rule) {
			ignored = !rule.negate
		}
	}
	return ignored
}

func matchesIgnoreRule(relative string, isDir bool, rule ignoreRule) bool {
	if rule.dirOnly && !isDir {
		return false
	}

	if rule.anchored || strings.Contains(rule.pattern, "/") {
		matched, _ := path.Match(rule.pattern, relative)
		return matched
	}

	segments := strings.Split(relative, "/")
	for _, segment := range segments {
		matched, _ := path.Match(rule.pattern, segment)
		if matched {
			return true
		}
	}
	return false
}

func readRevision(root string) (string, model.State, []model.Diagnostic) {
	gitDirectory := filepath.Join(root, ".git")
	info, err := os.Lstat(gitDirectory)
	if os.IsNotExist(err) {
		return "", model.StateUnknown, nil
	}
	if err != nil {
		return "", model.StateUnknown, []model.Diagnostic{{
			Code:     "git_metadata_unreadable",
			Severity: "warning",
			Message:  "Git metadata could not be inspected.",
		}}
	}
	if !info.IsDir() || isLinkInfo(info) {
		return "", model.StateUnknown, []model.Diagnostic{{
			Code:     "git_layout_unsupported",
			Severity: "info",
			Message:  "Git metadata is not a regular directory inside the scan root, so the revision was not followed.",
		}}
	}

	head, err := readBoundedFileUnder(root, ".git/HEAD", 4<<10)
	if err != nil {
		return "", model.StateUnknown, []model.Diagnostic{{
			Code:     "git_head_unreadable",
			Severity: "warning",
			Message:  "Git HEAD could not be read safely.",
		}}
	}
	headValue := strings.TrimSpace(string(head))
	if isHexRevision(headValue) {
		return strings.ToLower(headValue), model.StateObserved, nil
	}
	if !strings.HasPrefix(headValue, "ref: ") {
		return "", model.StateUnknown, []model.Diagnostic{{
			Code:     "git_head_unsupported",
			Severity: "info",
			Message:  "Git HEAD used an unsupported representation.",
		}}
	}

	reference := strings.TrimSpace(strings.TrimPrefix(headValue, "ref: "))
	if !strings.HasPrefix(reference, "refs/") || strings.Contains(reference, "..") || strings.ContainsRune(reference, '\\') {
		return "", model.StateUnknown, []model.Diagnostic{{
			Code:     "git_reference_rejected",
			Severity: "warning",
			Message:  "Git HEAD referenced a path outside the supported metadata layout.",
		}}
	}

	referencePath := path.Join(".git", reference)
	value, err := readBoundedFileUnder(root, referencePath, 4<<10)
	if err == nil && isHexRevision(strings.TrimSpace(string(value))) {
		return strings.ToLower(strings.TrimSpace(string(value))), model.StateObserved, nil
	}

	packed, packedErr := readBoundedFileUnder(root, ".git/packed-refs", maxGitMetadataBytes)
	if packedErr == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(packed)))
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) == 2 && fields[1] == reference && isHexRevision(fields[0]) {
				return strings.ToLower(fields[0]), model.StateObserved, nil
			}
		}
	}

	return "", model.StateUnknown, []model.Diagnostic{{
		Code:     "git_reference_unresolved",
		Severity: "info",
		Message:  "The current Git revision could not be resolved within the scan root.",
	}}
}

func readBoundedFileUnder(root, relative string, limit int64) ([]byte, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relative))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("path is outside the supported root")
	}

	current := root
	components := strings.Split(cleaned, string(os.PathSeparator))
	var info fs.FileInfo
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, fmt.Errorf("path contains an unsupported component")
		}
		current = filepath.Join(current, component)
		var err error
		info, err = os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if isLinkInfo(info) {
			return nil, fmt.Errorf("path contains a link or reparse point")
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, fmt.Errorf("path parent is not a directory")
		}
	}
	if info == nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds read limit")
	}

	file, err := os.Open(current)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("file exceeds read limit")
	}
	return content, nil
}

func isHexRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, character := range value {
		if !unicode.IsDigit(character) && (character < 'a' || character > 'f') && (character < 'A' || character > 'F') {
			return false
		}
	}
	return true
}
