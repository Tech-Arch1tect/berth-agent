package grypescanner

import "time"

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
	ID               string  `json:"id"`
	Severity         string  `json:"severity"`
	Package          string  `json:"package"`
	InstalledVersion string  `json:"installed_version"`
	FixedVersion     string  `json:"fixed_version,omitempty"`
	Description      string  `json:"description,omitempty"`
	DataSource       string  `json:"data_source,omitempty"`
	CVSS             float64 `json:"cvss,omitempty"`
	Location         string  `json:"location,omitempty"`
	LayerID          string  `json:"layer_id,omitempty"`
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
	Vulnerability GrypeVulnerability `json:"vulnerability"`
	Artifact      GrypeArtifact      `json:"artifact"`
}

type GrypeVulnerability struct {
	ID          string      `json:"id"`
	DataSource  string      `json:"dataSource"`
	Severity    string      `json:"severity"`
	Description string      `json:"description,omitempty"`
	CVSS        []GrypeCVSS `json:"cvss,omitempty"`
	Fix         GrypeFix    `json:"fix"`
}

type GrypeCVSS struct {
	Version string           `json:"version,omitempty"`
	Metrics GrypeCVSSMetrics `json:"metrics"`
}

type GrypeCVSSMetrics struct {
	BaseScore float64 `json:"baseScore"`
}

type GrypeFix struct {
	Versions []string `json:"versions,omitempty"`
}

type GrypeArtifact struct {
	Name      string          `json:"name"`
	Version   string          `json:"version"`
	Locations []GrypeLocation `json:"locations,omitempty"`
}

type GrypeLocation struct {
	Path    string `json:"path,omitempty"`
	LayerID string `json:"layerId,omitempty"`
}
