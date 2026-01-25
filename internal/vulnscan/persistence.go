package vulnscan

import (
	"encoding/json"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

type ScanPersistence struct {
	persistenceDir string
	logger         *logging.Logger
}

func NewScanPersistence(persistenceDir string, logger *logging.Logger) (*ScanPersistence, error) {
	if err := os.MkdirAll(persistenceDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create persistence directory: %w", err)
	}

	return &ScanPersistence{
		persistenceDir: persistenceDir,
		logger:         logger,
	}, nil
}

func (p *ScanPersistence) PersistScan(scan *Scan) error {
	filename := p.scanFilename(scan.ID)

	data, err := json.MarshalIndent(scan, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal scan: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write scan file: %w", err)
	}

	p.logger.Debug("persisted scan to disk",
		zap.String("scan_id", scan.ID),
		zap.String("filename", filename),
	)

	return nil
}

func (p *ScanPersistence) LoadScan(scanID string) (*Scan, error) {
	filename := p.scanFilename(scanID)

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read scan file: %w", err)
	}

	var scan Scan
	if err := json.Unmarshal(data, &scan); err != nil {
		return nil, fmt.Errorf("failed to unmarshal scan: %w", err)
	}

	return &scan, nil
}

func (p *ScanPersistence) LoadAllScans() ([]*Scan, error) {
	entries, err := os.ReadDir(p.persistenceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Scan{}, nil
		}
		return nil, fmt.Errorf("failed to read persistence directory: %w", err)
	}

	var scans []*Scan
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		scanID := strings.TrimSuffix(entry.Name(), ".json")
		scan, err := p.LoadScan(scanID)
		if err != nil {
			p.logger.Warn("failed to load scan file",
				zap.String("filename", entry.Name()),
				zap.Error(err),
			)
			continue
		}
		if scan != nil {
			scans = append(scans, scan)
		}
	}

	return scans, nil
}

func (p *ScanPersistence) DeleteScan(scanID string) error {
	filename := p.scanFilename(scanID)

	if err := os.Remove(filename); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to delete scan file: %w", err)
	}

	p.logger.Debug("deleted scan from disk",
		zap.String("scan_id", scanID),
	)

	return nil
}

func (p *ScanPersistence) scanFilename(scanID string) string {
	return filepath.Join(p.persistenceDir, scanID+".json")
}
