package vulnscan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

	serviceImages, err := s.getStackServiceImages(stackName, serviceFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to get images for stack: %w", err)
	}

	imageSet := make(map[string]bool)
	images := make([]string, 0, len(serviceImages))
	for _, si := range serviceImages {
		if !imageSet[si.Image] {
			imageSet[si.Image] = true
			images = append(images, si.Image)
		}
	}
	sort.Strings(images)

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
		ServiceImages: serviceImages,
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

func (s *Service) getStackServiceImages(stackName string, serviceFilter []string) ([]ServiceImage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stackDir := filepath.Join(s.stackLocation, stackName)

	args := []string{"compose", "ps", "-a", "--no-trunc", "--format", "json"}
	args = append(args, serviceFilter...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = stackDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list compose services: %w", err)
	}

	type psRow struct {
		Service string `json:"Service"`
		Image   string `json:"Image"`
	}

	var rows []psRow
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &rows); err != nil {
			return nil, fmt.Errorf("failed to parse compose ps output: %w", err)
		}
	} else {
		for _, line := range bytes.Split(trimmed, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var row psRow
			if err := json.Unmarshal(line, &row); err != nil {
				return nil, fmt.Errorf("failed to parse compose ps line: %w", err)
			}
			rows = append(rows, row)
		}
	}

	seen := make(map[string]bool)
	serviceImages := make([]ServiceImage, 0, len(rows))
	digests := make(map[string]imageIdentity)
	for _, row := range rows {
		if row.Image == "" {
			continue
		}
		key := row.Service + "|" + row.Image
		if seen[key] {
			continue
		}
		seen[key] = true

		identity, ok := digests[row.Image]
		if !ok {
			identity = s.resolveImageIdentity(ctx, row.Image)
			digests[row.Image] = identity
		}

		serviceImages = append(serviceImages, ServiceImage{
			Service: row.Service,
			Image:   identity.name,
			Digest:  identity.digest,
		})
	}

	sort.Slice(serviceImages, func(i, j int) bool {
		if serviceImages[i].Service != serviceImages[j].Service {
			return serviceImages[i].Service < serviceImages[j].Service
		}
		return serviceImages[i].Image < serviceImages[j].Image
	})

	return serviceImages, nil
}

type imageIdentity struct {
	name   string
	digest string
}

func (s *Service) resolveImageIdentity(ctx context.Context, imageRef string) imageIdentity {
	identity := imageIdentity{name: imageRef}

	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .RepoDigests}}", imageRef)
	output, err := cmd.Output()
	if err != nil {
		s.logger.Debug("failed to resolve image digest", zap.String("image", imageRef), zap.Error(err))
		return identity
	}
	var repoDigests []string
	if err := json.Unmarshal(bytes.TrimSpace(output), &repoDigests); err != nil || len(repoDigests) == 0 {
		return identity
	}

	repo := imageRef
	if idx := strings.LastIndex(repo, ":"); idx > strings.LastIndex(repo, "/") {
		repo = repo[:idx]
	}
	chosen := repoDigests[0]
	for _, rd := range repoDigests {
		if at := strings.Index(rd, "@"); at > 0 && rd[:at] == repo {
			chosen = rd
			break
		}
	}
	if at := strings.Index(chosen, "@"); at > 0 {
		identity.digest = chosen[at+1:]
		if isImageIDRef(imageRef) {
			identity.name = chosen[:at]
		}
	}
	return identity
}

func isImageIDRef(ref string) bool {
	ref = strings.TrimPrefix(ref, "sha256:")
	if len(ref) < 12 || len(ref) > 64 {
		return false
	}
	for _, r := range ref {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (s *Service) executeScan(scan *Scan) {
	ctx, cancel := context.WithTimeout(context.Background(), s.totalTimeout)
	defer cancel()

	scan.Status = ScanStatusRunning
	if err := s.persistence.PersistScan(scan); err != nil {
		s.logger.Warn("failed to persist scan status update", zap.String("scan_id", scan.ID), zap.Error(err))
	}

	digestByImage := make(map[string]string, len(scan.ServiceImages))
	for _, si := range scan.ServiceImages {
		if si.Digest != "" {
			digestByImage[si.Image] = si.Digest
		}
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

		result, scannerInfo := s.scanSingleImage(ctx, imageName)
		result.Digest = digestByImage[imageName]
		allVulnerabilities = append(allVulnerabilities, result)

		s.mutex.Lock()
		scan.ScannedImages++
		scan.Results = allVulnerabilities
		if scannerInfo != nil && scan.ScannerVersion == "" {
			scan.ScannerVersion = scannerInfo.Version
			scan.ScannerDBBuilt = scannerInfo.DBBuilt
		}
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

func (s *Service) scanSingleImage(ctx context.Context, imageName string) (ImageResult, *ScannerInfo) {
	imageCtx, cancel := context.WithTimeout(ctx, s.perImageTimeout)
	defer cancel()

	s.logger.Debug("scanning image", zap.String("image", imageName))

	vulnerabilities, scannerInfo, err := s.client.ScanImage(imageCtx, imageName)
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
		}, nil
	}

	return ImageResult{
		ImageName:       imageName,
		Status:          ImageStatusCompleted,
		Vulnerabilities: vulnerabilities,
		ScannedAt:       time.Now(),
	}, scannerInfo
}
