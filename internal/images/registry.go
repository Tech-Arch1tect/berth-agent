package images

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/tech-arch1tect/berth-agent/internal/logging"
	"go.uber.org/zap"
)

const manifestAccept = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

const registryRequestTimeout = 30 * time.Second

type registryClient struct {
	logger *logging.Logger
}

func newRegistryClient(logger *logging.Logger) *registryClient {
	return &registryClient{logger: logger}
}

func (rc *registryClient) ResolveTagDigest(ctx context.Context, imageName string, cred *RegistryCredential, insecure bool) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return "", fmt.Errorf("invalid image reference %q: %w", imageName, err)
	}

	tag := "latest"
	if tagged, ok := reference.TagNameOnly(named).(reference.Tagged); ok {
		tag = tagged.Tag()
	}

	domain := reference.Domain(named)
	repoPath := reference.Path(named)

	apiHost := domain
	if domain == "docker.io" {
		apiHost = "registry-1.docker.io"
	}

	if !insecure {
		return rc.resolveViaScheme(ctx, "https", apiHost, repoPath, tag, cred, false)
	}

	digest, httpsErr := rc.resolveViaScheme(ctx, "https", apiHost, repoPath, tag, cred, true)
	if httpsErr == nil {
		return digest, nil
	}
	rc.logger.Debug("HTTPS lookup against insecure registry failed, retrying over HTTP",
		zap.String("registry", apiHost),
		zap.Error(httpsErr),
	)
	digest, httpErr := rc.resolveViaScheme(ctx, "http", apiHost, repoPath, tag, cred, false)
	if httpErr != nil {
		return "", fmt.Errorf("registry lookup failed over HTTPS (%v) and HTTP (%v)", httpsErr, httpErr)
	}
	return digest, nil
}

func (rc *registryClient) resolveViaScheme(ctx context.Context, scheme, apiHost, repoPath, tag string, cred *RegistryCredential, skipTLSVerify bool) (string, error) {
	client := &http.Client{Timeout: registryRequestTimeout}
	if skipTLSVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	manifestURL := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, apiHost, repoPath, url.PathEscape(tag))

	digest, challenge, err := rc.fetchDigest(ctx, client, manifestURL, "")
	if err != nil {
		return "", err
	}
	if challenge == "" {
		return digest, nil
	}

	authorization, err := rc.answerChallenge(ctx, client, challenge, repoPath, cred)
	if err != nil {
		return "", err
	}

	digest, challenge, err = rc.fetchDigest(ctx, client, manifestURL, authorization)
	if err != nil {
		return "", err
	}
	if challenge != "" {
		return "", fmt.Errorf("registry rejected credentials for %s", apiHost)
	}
	return digest, nil
}

func (rc *registryClient) fetchDigest(ctx context.Context, client *http.Client, manifestURL, authorization string) (digest string, challenge string, err error) {
	resp, err := rc.doManifestRequest(ctx, client, http.MethodHead, manifestURL, authorization)
	if err != nil {
		return "", "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return "", resp.Header.Get("WWW-Authenticate"), nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return "", "", fmt.Errorf("registry returned status %d for %s", resp.StatusCode, manifestURL)
	}

	if d := resp.Header.Get("Docker-Content-Digest"); d != "" {
		return d, "", nil
	}

	resp, err = rc.doManifestRequest(ctx, client, http.MethodGet, manifestURL, authorization)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", "", fmt.Errorf("registry returned status %d for %s", resp.StatusCode, manifestURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read manifest body: %w", err)
	}
	if d := resp.Header.Get("Docker-Content-Digest"); d != "" {
		return d, "", nil
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(body)), "", nil
}

func (rc *registryClient) doManifestRequest(ctx context.Context, client *http.Client, method, manifestURL, authorization string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", manifestAccept)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return client.Do(req)
}

func (rc *registryClient) answerChallenge(ctx context.Context, client *http.Client, challenge, repoPath string, cred *RegistryCredential) (string, error) {
	scheme, params := parseAuthChallenge(challenge)

	switch scheme {
	case "basic":
		if cred == nil {
			return "", fmt.Errorf("registry requires authentication but no credential is configured")
		}
		req, _ := http.NewRequest(http.MethodGet, "http://placeholder", nil)
		req.SetBasicAuth(cred.Username, cred.Password)
		return req.Header.Get("Authorization"), nil
	case "bearer":
		token, err := rc.fetchBearerToken(ctx, client, params, repoPath, cred)
		if err != nil {
			return "", err
		}
		return "Bearer " + token, nil
	default:
		return "", fmt.Errorf("unsupported registry auth scheme %q", scheme)
	}
}

func (rc *registryClient) fetchBearerToken(ctx context.Context, client *http.Client, params map[string]string, repoPath string, cred *RegistryCredential) (string, error) {
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("bearer challenge missing realm")
	}

	tokenURL, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("invalid bearer realm %q: %w", realm, err)
	}
	query := tokenURL.Query()
	if service := params["service"]; service != "" {
		query.Set("service", service)
	}
	query.Set("scope", fmt.Sprintf("repository:%s:pull", repoPath))
	tokenURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", err
	}
	if cred != nil {
		req.SetBasicAuth(cred.Username, cred.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var tokenResp struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}
	if tokenResp.Token != "" {
		return tokenResp.Token, nil
	}
	if tokenResp.AccessToken != "" {
		return tokenResp.AccessToken, nil
	}
	return "", fmt.Errorf("token endpoint returned no token")
}

func parseAuthChallenge(header string) (scheme string, params map[string]string) {
	params = make(map[string]string)

	scheme, rest, _ := strings.Cut(strings.TrimSpace(header), " ")
	scheme = strings.ToLower(scheme)

	for _, part := range splitChallengeParams(rest) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		params[key] = value
	}
	return scheme, params
}

func splitChallengeParams(s string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			current.WriteRune(r)
		case r == ',' && !inQuotes:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}
