package vulnscan

import (
	"berth-agent/internal/logging"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type GrypeScannerClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	logger     *logging.Logger
}

type scanRequest struct {
	Image string `json:"image"`
}

type scanResponse struct {
	Image           string          `json:"image"`
	Status          string          `json:"status"`
	Error           string          `json:"error,omitempty"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities,omitempty"`
	ScannedAt       time.Time       `json:"scanned_at"`
}

type healthResponse struct {
	Status    string `json:"status"`
	Available bool   `json:"available"`
}

func NewGrypeScannerClient(baseURL, token string, logger *logging.Logger) *GrypeScannerClient {
	return &GrypeScannerClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 15 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		},
		logger: logger,
	}
}

func (c *GrypeScannerClient) IsAvailable(ctx context.Context) bool {
	healthCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(healthCtx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		c.logger.Debug("failed to create health request", zap.Error(err))
		return false
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logger.Debug("grype-scanner health check failed", zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	var health healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return false
	}

	return health.Available
}

func (c *GrypeScannerClient) ScanImage(ctx context.Context, imageName string) ([]Vulnerability, error) {
	reqBody := scanRequest{Image: imageName}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/scan", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	c.logger.Debug("calling grype-scanner",
		zap.String("image", imageName),
		zap.String("url", c.baseURL+"/scan"),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call grype-scanner: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grype-scanner returned status %d: %s", resp.StatusCode, string(body))
	}

	var scanResp scanResponse
	if err := json.Unmarshal(body, &scanResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if scanResp.Status == "failed" || scanResp.Status == "timeout" {
		return nil, fmt.Errorf("scan %s: %s", scanResp.Status, scanResp.Error)
	}

	return scanResp.Vulnerabilities, nil
}
