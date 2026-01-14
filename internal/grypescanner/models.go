package grypescanner

import (
	"encoding/json"
	"time"
)

type ScanRequest struct {
	Image string `json:"image"`
}

type ScanResponse struct {
	Image           string          `json:"image"`
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

type HealthResponse struct {
	Status    string `json:"status"`
	Available bool   `json:"available"`
}

const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusTimeout   = "timeout"
)

const (
	SeverityCritical   = "Critical"
	SeverityHigh       = "High"
	SeverityMedium     = "Medium"
	SeverityLow        = "Low"
	SeverityNegligible = "Negligible"
	SeverityUnknown    = "Unknown"
)

type GrypeOutput struct {
	Matches []GrypeMatch `json:"matches"`
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
