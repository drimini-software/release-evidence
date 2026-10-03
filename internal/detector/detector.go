package detector

import (
	"path"
	"sort"
	"strings"

	"github.com/drimini-software/release-evidence/internal/model"
	"github.com/drimini-software/release-evidence/internal/repository"
)

const maxCitationsPerFinding = 12

type rule struct {
	id             string
	version        string
	category       string
	title          string
	searchBoundary []string
	limitation     string
	match          func(string) bool
}

func Evaluate(snapshot repository.Snapshot) []model.Finding {
	rules := initialRules()
	findings := make([]model.Finding, 0, len(rules))
	for _, rule := range rules {
		findings = append(findings, evaluateRule(snapshot, rule))
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	return findings
}

func evaluateRule(snapshot repository.Snapshot, current rule) model.Finding {
	matches := make([]string, 0)
	for _, candidate := range snapshot.Paths {
		if current.match(candidate) {
			matches = append(matches, candidate)
		}
	}

	finding := model.Finding{
		ID:             current.id,
		RuleVersion:    current.version,
		Category:       current.category,
		Title:          current.title,
		Limitations:    current.limitation,
		SearchBoundary: append([]string{}, current.searchBoundary...),
		Citations:      make([]model.Citation, 0),
	}

	if len(matches) > 0 {
		finding.State = model.StateObserved
		finding.Confidence = model.ConfidenceHigh
		finding.Explanation = "Recognized evidence was observed at the cited repository paths."
		for _, match := range matches {
			if len(finding.Citations) == maxCitationsPerFinding {
				break
			}
			finding.Citations = append(finding.Citations, model.Citation{Path: match})
		}
		return finding
	}

	if snapshot.Complete {
		finding.State = model.StateNotObserved
		finding.Confidence = model.ConfidenceMedium
		finding.Explanation = "No recognized evidence was observed within the supported search boundary."
		return finding
	}

	finding.State = model.StateUnknown
	finding.Confidence = model.ConfidenceLow
	finding.Explanation = "The repository scan was incomplete, so absence cannot be evaluated responsibly."
	return finding
}

func initialRules() []rule {
	return []rule{
		{
			id:             "build.lockfile",
			version:        "1",
			category:       "build",
			title:          "Dependency lock evidence",
			searchBoundary: []string{"Recognized lockfiles at any repository depth"},
			limitation:     "A lockfile can improve reproducibility, but its presence does not prove dependencies are safe, current, or installed from this revision.",
			match:          matchBase("pnpm-lock.yaml", "package-lock.json", "yarn.lock", "bun.lock", "bun.lockb", "go.sum", "cargo.lock", "poetry.lock", "uv.lock", "composer.lock", "gemfile.lock"),
		},
		{
			id:             "build.manifest",
			version:        "1",
			category:       "build",
			title:          "Build or dependency definition",
			searchBoundary: []string{"Recognized build and package manifests at any repository depth"},
			limitation:     "The scanner recognizes selected manifest filenames only and does not execute or validate their build instructions.",
			match:          matchBase("package.json", "go.mod", "cargo.toml", "pyproject.toml", "requirements.txt", "pom.xml", "build.gradle", "build.gradle.kts", "composer.json", "gemfile"),
		},
		{
			id:             "operations.deployment",
			version:        "1",
			category:       "operations",
			title:          "Deployment configuration",
			searchBoundary: []string{"Recognized deployment and container configuration filenames"},
			limitation:     "Repository configuration shows intended deployment mechanics, not the state, security, or correctness of a deployed environment.",
			match:          matchDeployment,
		},
		{
			id:             "operations.recovery",
			version:        "1",
			category:       "operations",
			title:          "Rollback or recovery guidance",
			searchBoundary: []string{"Documentation filenames containing rollback, recovery, restore, backup, or disaster-recovery terms"},
			limitation:     "Filename recognition does not establish that guidance is current, complete, or tested in the actual environment.",
			match:          matchRecovery,
		},
		{
			id:             "operations.runbook-handover",
			version:        "1",
			category:       "operations",
			title:          "Runbook or handover guidance",
			searchBoundary: []string{"Recognized runbook, operations, and handover document filenames"},
			limitation:     "Filename recognition does not verify that the document is sufficient for the receiving owner or matches deployed reality.",
			match:          matchOperationalDocument,
		},
		{
			id:             "ownership.metadata",
			version:        "1",
			category:       "ownership",
			title:          "Repository ownership metadata",
			searchBoundary: []string{"CODEOWNERS", ".github/CODEOWNERS", "docs/CODEOWNERS"},
			limitation:     "Ownership metadata may be stale and does not prove that the listed person or team accepts operational responsibility.",
			match:          matchExact("codeowners", ".github/codeowners", "docs/codeowners"),
		},
		{
			id:             "security.policy",
			version:        "1",
			category:       "security",
			title:          "Security reporting guidance",
			searchBoundary: []string{"SECURITY.md", ".github/SECURITY.md", "docs/SECURITY.md"},
			limitation:     "A policy file does not prove response capacity, response time, or secure implementation.",
			match:          matchExact("security.md", ".github/security.md", "docs/security.md"),
		},
		{
			id:             "verification.ci",
			version:        "1",
			category:       "verification",
			title:          "Continuous-integration configuration",
			searchBoundary: []string{"Recognized GitHub, GitLab, Azure, Bitbucket, Jenkins, and CircleCI configuration paths"},
			limitation:     "Configuration presence does not prove that checks ran, passed, or are required for the reviewed revision.",
			match:          matchCI,
		},
		{
			id:             "verification.tests",
			version:        "1",
			category:       "verification",
			title:          "Automated test sources",
			searchBoundary: []string{"Recognized test directories and common test filename suffixes"},
			limitation:     "Test files do not establish coverage, relevance, pass status, or execution against the release environment.",
			match:          matchTest,
		},
	}
}

func matchBase(names ...string) func(string) bool {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[strings.ToLower(name)] = struct{}{}
	}
	return func(candidate string) bool {
		_, ok := allowed[strings.ToLower(path.Base(candidate))]
		return ok
	}
}

func matchExact(names ...string) func(string) bool {
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[strings.ToLower(name)] = struct{}{}
	}
	return func(candidate string) bool {
		_, ok := allowed[strings.ToLower(candidate)]
		return ok
	}
}

func matchCI(candidate string) bool {
	lower := strings.ToLower(candidate)
	base := path.Base(lower)
	if strings.HasPrefix(lower, ".github/workflows/") && (strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")) {
		return true
	}
	switch lower {
	case ".gitlab-ci.yml", "azure-pipelines.yml", "bitbucket-pipelines.yml", ".circleci/config.yml":
		return true
	}
	return base == "jenkinsfile"
}

func matchTest(candidate string) bool {
	lower := strings.ToLower(candidate)
	segments := strings.Split(lower, "/")
	for _, segment := range segments[:len(segments)-1] {
		if segment == "test" || segment == "tests" || segment == "__tests__" || segment == "spec" {
			return true
		}
	}
	base := path.Base(lower)
	suffixes := []string{"_test.go", ".test.ts", ".test.tsx", ".test.js", ".test.jsx", ".spec.ts", ".spec.tsx", ".spec.js", ".spec.jsx", "_test.py", "test.py"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return false
}

func matchDeployment(candidate string) bool {
	lower := strings.ToLower(candidate)
	base := path.Base(lower)
	switch base {
	case "dockerfile", "compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml", "wrangler.toml", "wrangler.json", "wrangler.jsonc", "fly.toml", "vercel.json", "netlify.toml", "render.yaml", "app.yaml", "procfile":
		return true
	}
	return strings.HasPrefix(lower, "k8s/") || strings.HasPrefix(lower, "kubernetes/") || strings.HasPrefix(lower, "deploy/")
}

func matchRecovery(candidate string) bool {
	lower := strings.ToLower(path.Base(candidate))
	if !isDocument(lower) {
		return false
	}
	terms := []string{"rollback", "recovery", "restore", "backup", "disaster-recovery", "disaster_recovery"}
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func matchOperationalDocument(candidate string) bool {
	lower := strings.ToLower(path.Base(candidate))
	if !isDocument(lower) {
		return false
	}
	terms := []string{"runbook", "handover", "operations", "operating-guide", "operating_guide"}
	for _, term := range terms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func isDocument(base string) bool {
	extensions := []string{".md", ".txt", ".rst", ".adoc"}
	for _, extension := range extensions {
		if strings.HasSuffix(base, extension) {
			return true
		}
	}
	return false
}
