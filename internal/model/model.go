package model

// SchemaVersion is the version of the serialized evidence packet contract.
const SchemaVersion = "1"

// State describes what the scanner can responsibly say about one evidence rule.
type State string

const (
	StateObserved      State = "observed"
	StateNotObserved   State = "not_observed"
	StateUnknown       State = "unknown"
	StateNotApplicable State = "not_applicable"
)

// Confidence applies to the observation, not to the safety of the software.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type Tool struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	RuleSetVersion string `json:"rule_set_version"`
}

type Repository struct {
	Name             string `json:"name"`
	Revision         string `json:"revision,omitempty"`
	RevisionState    State  `json:"revision_state"`
	WorkingTreeState State  `json:"working_tree_state"`
	WorkingTreeNote  string `json:"working_tree_note"`
}

type Limits struct {
	MaxEntries        int   `json:"max_entries"`
	MaxDepth          int   `json:"max_depth"`
	MaxFileBytes      int64 `json:"max_file_bytes"`
	MaxTotalReadBytes int64 `json:"max_total_read_bytes"`
}

type Boundary struct {
	Root       string   `json:"root"`
	Exclusions []string `json:"exclusions"`
	Limits     Limits   `json:"limits"`
}

type Scan struct {
	GeneratedAt     string `json:"generated_at"`
	Complete        bool   `json:"complete"`
	EntriesVisited  int    `json:"entries_visited"`
	FilesConsidered int    `json:"files_considered"`
}

type Citation struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type Finding struct {
	ID             string     `json:"id"`
	RuleVersion    string     `json:"rule_version"`
	Category       string     `json:"category"`
	Title          string     `json:"title"`
	State          State      `json:"state"`
	Confidence     Confidence `json:"confidence"`
	Explanation    string     `json:"explanation"`
	Limitations    string     `json:"limitations"`
	SearchBoundary []string   `json:"search_boundary"`
	Citations      []Citation `json:"citations"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type Assessment struct {
	Status    string           `json:"status"`
	Context   string           `json:"context,omitempty"`
	Items     []AssessmentItem `json:"items,omitempty"`
	UpdatedAt string           `json:"updated_at,omitempty"`
}

type AssessmentItem struct {
	FindingID   string `json:"finding_id"`
	Disposition string `json:"disposition"`
	Rationale   string `json:"rationale,omitempty"`
	Owner       string `json:"owner,omitempty"`
	TargetDate  string `json:"target_date,omitempty"`
}

type Decision struct {
	Status    string `json:"status"`
	Rationale string `json:"rationale,omitempty"`
	Owner     string `json:"owner,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type Integrity struct {
	Algorithm string `json:"algorithm"`
	Scope     string `json:"scope"`
	Digest    string `json:"digest"`
	Note      string `json:"note"`
}

type Packet struct {
	SchemaVersion string       `json:"schema_version"`
	Tool          Tool         `json:"tool"`
	Repository    Repository   `json:"repository"`
	Boundary      Boundary     `json:"boundary"`
	Scan          Scan         `json:"scan"`
	Findings      []Finding    `json:"findings"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	Assessment    Assessment   `json:"assessment"`
	Decision      Decision     `json:"decision"`
	Integrity     Integrity    `json:"integrity"`
}
