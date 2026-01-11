package vulnscan

import (
	"berth-agent/internal/logging"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

type GrypeScanner struct {
	grypePath string
	available bool
	logger    *logging.Logger
}

func NewGrypeScanner(logger *logging.Logger) *GrypeScanner {
	scanner := &GrypeScanner{
		grypePath: "grype",
		available: false,
		logger:    logger,
	}
	scanner.checkAvailability()
	return scanner
}

func (g *GrypeScanner) checkAvailability() {
	path, err := exec.LookPath(g.grypePath)
	if err != nil {
		g.logger.Warn("grype not found in PATH - vulnerability scanning disabled",
			zap.Error(err),
		)
		g.available = false
		return
	}
	g.grypePath = path
	g.available = true
	g.logger.Info("grype vulnerability scanner available",
		zap.String("path", path),
	)
}

func (g *GrypeScanner) IsAvailable() bool {
	return g.available
}

func (g *GrypeScanner) ScanImage(ctx context.Context, imageName string) ([]Vulnerability, error) {
	if !g.available {
		return nil, fmt.Errorf("grype is not installed or not available in PATH")
	}

	g.logger.Debug("starting grype scan",
		zap.String("image", imageName),
	)

	cmd := exec.CommandContext(ctx, g.grypePath, imageName, "-o", "json", "--quiet")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("scan timed out for image %s", imageName)
	}
	if ctx.Err() == context.Canceled {
		return nil, fmt.Errorf("scan cancelled for image %s", imageName)
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			g.logger.Error("grype scan failed",
				zap.String("image", imageName),
				zap.Int("exit_code", exitErr.ExitCode()),
				zap.String("stderr", stderr.String()),
			)
			return nil, fmt.Errorf("grype scan failed: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("failed to execute grype: %w", err)
	}

	vulnerabilities, err := g.parseGrypeOutput(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse grype output: %w", err)
	}

	g.logger.Debug("grype scan completed",
		zap.String("image", imageName),
		zap.Int("vulnerabilities_found", len(vulnerabilities)),
	)

	return vulnerabilities, nil
}

func (g *GrypeScanner) parseGrypeOutput(data []byte) ([]Vulnerability, error) {
	if len(data) == 0 {
		return []Vulnerability{}, nil
	}

	var output GrypeOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, fmt.Errorf("failed to unmarshal grype JSON: %w", err)
	}

	vulnerabilities := make([]Vulnerability, 0, len(output.Matches))

	for _, match := range output.Matches {
		vuln := g.convertMatch(match)
		vulnerabilities = append(vulnerabilities, vuln)
	}

	return vulnerabilities, nil
}

func (g *GrypeScanner) convertMatch(match GrypeMatch) Vulnerability {
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

	vuln.CVSS = extractCVSSScore(match.Vulnerability.CVSS)

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
