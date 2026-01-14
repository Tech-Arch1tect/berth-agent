package vulnscan

import (
	"encoding/json"
	"time"
)

const (
	ScanStatusPending   = "pending"
	ScanStatusRunning   = "running"
	ScanStatusCompleted = "completed"
	ScanStatusFailed    = "failed"
)

const (
	ImageStatusPending   = "pending"
	ImageStatusScanning  = "scanning"
	ImageStatusCompleted = "completed"
	ImageStatusFailed    = "failed"
	ImageStatusTimeout   = "timeout"
)

const (
	SeverityCritical   = "Critical"
	SeverityHigh       = "High"
	SeverityMedium     = "Medium"
	SeverityLow        = "Low"
	SeverityNegligible = "Negligible"
	SeverityUnknown    = "Unknown"
)

type Scan struct {
	ID            string        `json:"id"`
	StackName     string        `json:"stack_name"`
	Status        string        `json:"status"`
	Images        []string      `json:"images"`
	TotalImages   int           `json:"total_images"`
	ScannedImages int           `json:"scanned_images"`
	StartedAt     time.Time     `json:"started_at"`
	CompletedAt   *time.Time    `json:"completed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
	Results       []ImageResult `json:"results,omitempty"`
}

type ImageResult struct {
	ImageName       string          `json:"image_name"`
	Status          string          `json:"status"`
	Error           string          `json:"error,omitempty"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities,omitempty"`
	ScannedAt       time.Time       `json:"scanned_at"`
}

type Vulnerability struct {
	ID               string          `json:"id"`
	Severity         string          `json:"severity"`
	Package          string          `json:"package"`
	InstalledVersion string          `json:"installed_version"`
	FixedVersion     string          `json:"fixed_version,omitempty"`
	Description      string          `json:"description,omitempty"`
	DataSource       string          `json:"data_source,omitempty"`
	CVSS             float64         `json:"cvss,omitempty"`
	Location         string          `json:"location,omitempty"`
	LayerID          string          `json:"layer_id,omitempty"`
	RawMatch         json.RawMessage `json:"raw_match,omitempty"`
}

type GetScanResponse struct {
	ID            string        `json:"id"`
	StackName     string        `json:"stack_name"`
	Status        string        `json:"status"`
	TotalImages   int           `json:"total_images"`
	ScannedImages int           `json:"scanned_images"`
	StartedAt     time.Time     `json:"started_at"`
	CompletedAt   *time.Time    `json:"completed_at,omitempty"`
	Error         string        `json:"error,omitempty"`
	Results       []ImageResult `json:"results,omitempty"`
}

type GrypeOutput struct {
	Matches    []GrypeMatch    `json:"matches"`
	Source     GrypeSource     `json:"source"`
	Distro     GrypeDistro     `json:"distro"`
	Descriptor GrypeDescriptor `json:"descriptor"`
}

type GrypeMatch struct {
	Vulnerability          GrypeVulnerability   `json:"vulnerability"`
	RelatedVulnerabilities []GrypeVulnerability `json:"relatedVulnerabilities,omitempty"`
	MatchDetails           []GrypeMatchDetail   `json:"matchDetails,omitempty"`
	Artifact               GrypeArtifact        `json:"artifact"`
}

type GrypeVulnerability struct {
	ID          string      `json:"id"`
	DataSource  string      `json:"dataSource"`
	Namespace   string      `json:"namespace,omitempty"`
	Severity    string      `json:"severity"`
	URLs        []string    `json:"urls,omitempty"`
	Description string      `json:"description,omitempty"`
	CVSS        []GrypeCVSS `json:"cvss,omitempty"`
	Fix         GrypeFix    `json:"fix"`
}

type GrypeCVSS struct {
	Source  string           `json:"source,omitempty"`
	Type    string           `json:"type,omitempty"`
	Version string           `json:"version,omitempty"`
	Vector  string           `json:"vector,omitempty"`
	Metrics GrypeCVSSMetrics `json:"metrics"`
}

type GrypeCVSSMetrics struct {
	BaseScore           float64 `json:"baseScore"`
	ExploitabilityScore float64 `json:"exploitabilityScore,omitempty"`
	ImpactScore         float64 `json:"impactScore,omitempty"`
}

type GrypeFix struct {
	Versions []string `json:"versions,omitempty"`
	State    string   `json:"state,omitempty"`
}

type GrypeMatchDetail struct {
	Type    string `json:"type,omitempty"`
	Matcher string `json:"matcher,omitempty"`
}

type GrypeArtifact struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Version   string          `json:"version"`
	Type      string          `json:"type,omitempty"`
	Locations []GrypeLocation `json:"locations,omitempty"`
	Language  string          `json:"language,omitempty"`
	Licenses  []string        `json:"licenses,omitempty"`
	CPEs      []string        `json:"cpes,omitempty"`
	PURL      string          `json:"purl,omitempty"`
}

type GrypeLocation struct {
	Path    string `json:"path,omitempty"`
	LayerID string `json:"layerId,omitempty"`
}

type GrypeSource struct {
	Type   string `json:"type,omitempty"`
	Target any    `json:"target,omitempty"`
}

type GrypeDistro struct {
	Name    string   `json:"name,omitempty"`
	Version string   `json:"version,omitempty"`
	IDLike  []string `json:"idLike,omitempty"`
}

type GrypeDescriptor struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}
