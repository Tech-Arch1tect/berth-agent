package vulnscan

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service struct {
	stackLocation   string
	persistenceDir  string
	perImageTimeout time.Duration
	totalTimeout    time.Duration

	client      *GrypeScannerClient
	persistence *ScanPersistence
	logger      *logging.Logger

	scans       map[string]*Scan
	activeScans map[string]string
	mutex       sync.RWMutex
}

type ServiceConfig struct {
	StackLocation     string
	PersistenceDir    string
	PerImageTimeout   time.Duration
	TotalTimeout      time.Duration
	GrypeScannerURL   string
	GrypeScannerToken string
}

func NewService(cfg ServiceConfig, logger *logging.Logger) (*Service, error) {
	persistence, err := NewScanPersistence(cfg.PersistenceDir, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise persistence: %w", err)
	}

	if cfg.GrypeScannerURL == "" {
		return nil, fmt.Errorf("GRYPE_SCANNER_URL is required")
	}

	svc := &Service{
		stackLocation:   cfg.StackLocation,
		persistenceDir:  cfg.PersistenceDir,
		perImageTimeout: cfg.PerImageTimeout,
		totalTimeout:    cfg.TotalTimeout,
		client:          NewGrypeScannerClient(cfg.GrypeScannerURL, cfg.GrypeScannerToken, logger),
		persistence:     persistence,
		logger:          logger,
		scans:           make(map[string]*Scan),
		activeScans:     make(map[string]string),
	}

	logger.Info("vulnscan using grype-scanner API",
		zap.String("url", cfg.GrypeScannerURL),
	)

	if err := svc.recoverScans(); err != nil {
		logger.Warn("failed to recover scans on startup", zap.Error(err))
	}

	return svc, nil
}

func (s *Service) recoverScans() error {
	scans, err := s.persistence.LoadAllScans()
	if err != nil {
		return err
	}

	for _, scan := range scans {
		if scan.Status == ScanStatusPending || scan.Status == ScanStatusRunning {
			scan.Status = ScanStatusFailed
			scan.Error = "agent restarted during scan"
			now := time.Now()
			scan.CompletedAt = &now

			if err := s.persistence.PersistScan(scan); err != nil {
				s.logger.Warn("failed to persist recovered scan",
					zap.String("scan_id", scan.ID),
					zap.Error(err),
				)
			}

			s.logger.Info("marked incomplete scan as failed",
				zap.String("scan_id", scan.ID),
				zap.String("stack_name", scan.StackName),
			)
		}

		s.mutex.Lock()
		s.scans[scan.ID] = scan
		s.mutex.Unlock()
	}

	s.logger.Info("recovered scans from disk", zap.Int("count", len(scans)))
	return nil
}

func (s *Service) IsAvailable() bool {
	return s.client.IsAvailable(context.Background())
}

func (s *Service) StartScan(ctx context.Context, stackName string, serviceFilter []string) (*Scan, error) {
	if !s.IsAvailable() {
		return nil, fmt.Errorf("vulnerability scanning is not available: grype not installed or grype-scanner unreachable")
	}

	stackPath := filepath.Join(s.stackLocation, stackName)
	if _, err := os.Stat(stackPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("stack '%s' not found", stackName)
	}

	s.mutex.Lock()
	if existingScanID, exists := s.activeScans[stackName]; exists {
		existingScan := s.scans[existingScanID]
		s.mutex.Unlock()
		return existingScan, nil
	}
	s.mutex.Unlock()

	images, err := s.getStackImages(stackName, serviceFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to get images for stack: %w", err)
	}

	if len(images) == 0 {
		return nil, fmt.Errorf("no images found in stack '%s'", stackName)
	}

	scanID := uuid.New().String()
	now := time.Now()

	scan := &Scan{
		ID:            scanID,
		StackName:     stackName,
		Status:        ScanStatusPending,
		Images:        images,
		TotalImages:   len(images),
		ScannedImages: 0,
		StartedAt:     now,
		Results:       []ImageResult{},
	}

	s.mutex.Lock()
	s.scans[scanID] = scan
	s.activeScans[stackName] = scanID
	s.mutex.Unlock()

	if err := s.persistence.PersistScan(scan); err != nil {
		s.logger.Warn("failed to persist new scan", zap.String("scan_id", scanID), zap.Error(err))
	}

	go s.executeScan(scan)

	s.logger.Info("started vulnerability scan",
		zap.String("scan_id", scanID),
		zap.String("stack_name", stackName),
		zap.Int("image_count", len(images)),
	)

	return scan, nil
}

func (s *Service) GetScan(scanID string) (*Scan, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	scan, exists := s.scans[scanID]
	return scan, exists
}

func (s *Service) getStackImages(stackName string, serviceFilter []string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := []string{"compose", "images"}
	args = append(args, serviceFilter...)
	args = append(args, "--format", "json")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = filepath.Join(s.stackLocation, stackName)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get compose images: %w", err)
	}

	var composeImages []struct {
		Repository string `json:"Repository"`
		Tag        string `json:"Tag"`
	}
	if err := json.Unmarshal(output, &composeImages); err != nil {
		return nil, fmt.Errorf("failed to parse compose images: %w", err)
	}

	imageSet := make(map[string]bool)
	for _, img := range composeImages {
		if img.Repository == "" {
			continue
		}
		imageName := img.Repository
		if img.Tag != "" {
			imageName = img.Repository + ":" + img.Tag
		}
		imageSet[imageName] = true
	}

	images := make([]string, 0, len(imageSet))
	for image := range imageSet {
		images = append(images, image)
	}

	return images, nil
}

func (s *Service) filterImages(available []string, filter []string) []string {
	filterSet := make(map[string]bool)
	for _, img := range filter {
		filterSet[img] = true
	}

	var result []string
	for _, img := range available {
		if filterSet[img] {
			result = append(result, img)
		}
	}
	return result
}

func (s *Service) executeScan(scan *Scan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.totalTimeout)
	defer cancel()

	scan.Status = ScanStatusRunning
	if err := s.persistence.PersistScan(scan); err != nil {
		s.logger.Warn("failed to persist scan status update", zap.String("scan_id", scan.ID), zap.Error(err))
	}

	var allVulnerabilities []ImageResult

	for _, imageName := range scan.Images {
		if ctx.Err() != nil {
			s.logger.Warn("scan timed out",
				zap.String("scan_id", scan.ID),
				zap.String("current_image", imageName),
			)
			break
		}

		result := s.scanSingleImage(ctx, imageName)
		allVulnerabilities = append(allVulnerabilities, result)

		s.mutex.Lock()
		scan.ScannedImages++
		scan.Results = allVulnerabilities
		s.mutex.Unlock()

		if err := s.persistence.PersistScan(scan); err != nil {
			s.logger.Warn("failed to persist scan progress", zap.String("scan_id", scan.ID), zap.Error(err))
		}
	}

	now := time.Now()
	s.mutex.Lock()
	scan.CompletedAt = &now
	scan.Results = allVulnerabilities

	if ctx.Err() == context.DeadlineExceeded {
		scan.Status = ScanStatusFailed
		scan.Error = "scan timed out"
	} else {
		scan.Status = ScanStatusCompleted
	}

	delete(s.activeScans, scan.StackName)
	s.mutex.Unlock()

	if err := s.persistence.PersistScan(scan); err != nil {
		s.logger.Warn("failed to persist completed scan", zap.String("scan_id", scan.ID), zap.Error(err))
	}

	s.logger.Info("vulnerability scan completed",
		zap.String("scan_id", scan.ID),
		zap.String("stack_name", scan.StackName),
		zap.String("status", scan.Status),
		zap.Int("images_scanned", scan.ScannedImages),
	)
}

func (s *Service) scanSingleImage(ctx context.Context, imageName string) ImageResult {
	imageCtx, cancel := context.WithTimeout(ctx, s.perImageTimeout)
	defer cancel()

	s.logger.Debug("scanning image", zap.String("image", imageName))

	vulnerabilities, err := s.client.ScanImage(imageCtx, imageName)
	if err != nil {
		s.logger.Warn("failed to scan image",
			zap.String("image", imageName),
			zap.Error(err),
		)

		status := ImageStatusFailed
		if imageCtx.Err() == context.DeadlineExceeded {
			status = ImageStatusTimeout
		}

		return ImageResult{
			ImageName: imageName,
			Status:    status,
			Error:     err.Error(),
			ScannedAt: time.Now(),
		}
	}

	return ImageResult{
		ImageName:       imageName,
		Status:          ImageStatusCompleted,
		Vulnerabilities: vulnerabilities,
		ScannedAt:       time.Now(),
	}
}
