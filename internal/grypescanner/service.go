package grypescanner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"os/exec"
	"strings"
	"time"

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

	vulnerabilities, err := s.parseGrypeOutput(stdout.Bytes())
	if err != nil {
		response.Status = StatusFailed
		response.Error = fmt.Sprintf("failed to parse output: %v", err)
		return response, nil
	}

	response.Status = StatusCompleted
	response.Vulnerabilities = vulnerabilities

	s.logger.Info("grype scan completed",
		zap.String("image", imageName),
		zap.Int("vulnerabilities_found", len(vulnerabilities)),
	)

	return response, nil
}

func (s *Service) parseGrypeOutput(data []byte) ([]Vulnerability, error) {
	if len(data) == 0 {
		return []Vulnerability{}, nil
	}

	var output GrypeOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("failed to unmarshal grype JSON: %w", err)
	}

	vulnerabilities := make([]Vulnerability, 0, len(output.Matches))
	for _, match := range output.Matches {
		vuln := s.convertMatch(match)
		vulnerabilities = append(vulnerabilities, vuln)
	}

	return vulnerabilities, nil
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
	var bestScore float64
	var bestVersion string

	for _, entry := range cvssEntries {
		if bestVersion == "" || entry.Version > bestVersion {
			bestVersion = entry.Version
			bestScore = entry.Metrics.BaseScore
		}
	}

	return bestScore
}
