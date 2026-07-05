package grypescanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"go.uber.org/zap"
)

type Service struct {
	grypePath string
	available bool
	timeout   time.Duration
	logger    *logging.Logger
}

func NewService(logger *logging.Logger) *Service {
	svc := &Service{
		grypePath: "grype",
		available: false,
		timeout:   10 * time.Minute,
		logger:    logger,
	}
	svc.checkAvailability()
	return svc
}

func (s *Service) checkAvailability() {
	path, err := exec.LookPath(s.grypePath)
	if err != nil {
		s.logger.Warn("grype not found in PATH",
			zap.Error(err),
		)
		s.available = false
		return
	}
	s.grypePath = path
	s.available = true
	s.logger.Info("grype scanner available",
		zap.String("path", path),
	)
}

func (s *Service) IsAvailable() bool {
	return s.available
}

func (s *Service) ScanImage(ctx context.Context, imageName string) (*ScanResponse, error) {
	if !s.available {
		return nil, fmt.Errorf("grype is not available")
	}

	scanCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	s.logger.Info("starting grype scan",
		zap.String("image", imageName),
	)

	cmd := exec.CommandContext(scanCtx, s.grypePath, imageName, "-o", "json", "--quiet")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	response := &ScanResponse{
		Image:     imageName,
		ScannedAt: time.Now(),
	}

	if scanCtx.Err() == context.DeadlineExceeded {
		response.Status = StatusTimeout
		response.Error = "scan timed out"
		return response, nil
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			s.logger.Error("grype scan failed",
				zap.String("image", imageName),
				zap.Int("exit_code", exitErr.ExitCode()),
				zap.String("stderr", stderr.String()),
			)
		}
		response.Status = StatusFailed
		response.Error = strings.TrimSpace(stderr.String())
		if response.Error == "" {
			response.Error = err.Error()
		}
		return response, nil
	}

	vulnerabilities, descriptor, err := s.parseGrypeOutput(stdout.Bytes())
	if err != nil {
		response.Status = StatusFailed
		response.Error = fmt.Sprintf("failed to parse output: %v", err)
		return response, nil
	}

	response.Status = StatusCompleted
	response.Vulnerabilities = vulnerabilities
	response.ScannerVersion = descriptor.Version
	response.ScannerDBBuilt = parseDBBuiltDate(descriptor.DB)

	s.logger.Info("grype scan completed",
		zap.String("image", imageName),
		zap.Int("vulnerabilities_found", len(vulnerabilities)),
	)

	return response, nil
}

func (s *Service) parseGrypeOutput(data []byte) ([]Vulnerability, GrypeDescriptor, error) {
	if len(data) == 0 {
		return []Vulnerability{}, GrypeDescriptor{}, nil
	}

	var output GrypeOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, GrypeDescriptor{}, fmt.Errorf("failed to unmarshal grype JSON: %w", err)
	}

	vulnerabilities := make([]Vulnerability, 0, len(output.Matches))
	seen := make(map[string]bool, len(output.Matches))
	for _, match := range output.Matches {
		vuln := s.convertMatch(match)
		key := vuln.ID + "|" + vuln.Package + "|" + vuln.InstalledVersion + "|" + vuln.Location
		if seen[key] {
			continue
		}
		seen[key] = true
		vulnerabilities = append(vulnerabilities, vuln)
	}

	sort.SliceStable(vulnerabilities, func(i, j int) bool {
		ri, rj := severityRank(vulnerabilities[i].Severity), severityRank(vulnerabilities[j].Severity)
		if ri != rj {
			return ri < rj
		}
		if vulnerabilities[i].ID != vulnerabilities[j].ID {
			return vulnerabilities[i].ID < vulnerabilities[j].ID
		}
		return vulnerabilities[i].Package < vulnerabilities[j].Package
	})

	return vulnerabilities, output.Descriptor, nil
}

func severityRank(severity string) int {
	switch severity {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 3
	case SeverityNegligible:
		return 4
	default:
		return 5
	}
}

func parseDBBuiltDate(raw json.RawMessage) *time.Time {
	if len(raw) == 0 {
		return nil
	}
	var direct struct {
		Built time.Time `json:"built"`
	}
	if err := json.Unmarshal(raw, &direct); err == nil && !direct.Built.IsZero() {
		return &direct.Built
	}
	var nested struct {
		Status struct {
			Built time.Time `json:"built"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &nested); err == nil && !nested.Status.Built.IsZero() {
		return &nested.Status.Built
	}
	return nil
}

func (s *Service) convertMatch(match GrypeMatch) Vulnerability {
	vuln := Vulnerability{
		ID:               match.Vulnerability.ID,
		Severity:         normaliseSeverity(match.Vulnerability.Severity),
		Package:          match.Artifact.Name,
		InstalledVersion: match.Artifact.Version,
		Description:      match.Vulnerability.Description,
		DataSource:       match.Vulnerability.DataSource,
	}

	if len(match.Vulnerability.Fix.Versions) > 0 {
		vuln.FixedVersion = match.Vulnerability.Fix.Versions[0]
	}

	if len(match.Artifact.Locations) > 0 {
		vuln.Location = match.Artifact.Locations[0].Path
		vuln.LayerID = match.Artifact.Locations[0].LayerID
	}

	vuln.CVSS = extractCVSSScore(match.Vulnerability.CVSS)

	if rawMatch, err := json.Marshal(match); err == nil {
		vuln.RawMatch = rawMatch
	}

	return vuln
}

func normaliseSeverity(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return SeverityCritical
	case "high":
		return SeverityHigh
	case "medium":
		return SeverityMedium
	case "low":
		return SeverityLow
	case "negligible":
		return SeverityNegligible
	default:
		return SeverityUnknown
	}
}

func extractCVSSScore(cvssEntries []GrypeCVSS) float64 {
	var maxScore float64
	for _, entry := range cvssEntries {
		if entry.Metrics.BaseScore > maxScore {
			maxScore = entry.Metrics.BaseScore
		}
	}
	return maxScore
}
