package backup

import (
	"fmt"
	"net/url"
	"strings"
)

const redactedPlaceholder = "[redacted]"

type secretRedactor struct {
	tokens []string
}

func newSecretRedactor(password string, repositories ...*S3Repository) *secretRedactor {
	redactor := &secretRedactor{}
	redactor.addToken(password)
	for _, repository := range repositories {
		if repository == nil {
			continue
		}
		redactor.addToken(repository.AccessKeyID)
		redactor.addToken(repository.SecretKey)
		redactor.addToken(repository.URL)
		redactor.addToken(strings.TrimPrefix(repository.URL, "s3:"))
		endpoint, err := url.Parse(strings.TrimPrefix(repository.URL, "s3:"))
		if err != nil {
			continue
		}
		redactor.addToken(endpoint.Hostname())
		redactor.addToken(firstPathSegment(endpoint.Path))
	}
	return redactor
}

func (r *secretRedactor) addToken(token string) {
	if token != "" {
		r.tokens = append(r.tokens, token)
	}
}

func (r *secretRedactor) line(value string) string {
	for _, token := range r.tokens {
		value = strings.ReplaceAll(value, token, redactedPlaceholder)
	}
	return value
}

func (r *secretRedactor) apply(value string) string {
	return r.line(value)
}

func firstPathSegment(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return ""
	}
	if segment, _, found := strings.Cut(trimmed, "/"); found {
		return segment
	}
	return trimmed
}

func resticOperationError(action string, result bufferedResticResult) error {
	switch result.exitCode {
	case resticExitRepoDoesNotExist:
		return fmt.Errorf("%s failed: %w", action, errBackupRepositoryMissing)
	case resticExitWrongPassword:
		return fmt.Errorf("%s failed: %w", action, errBackupPasswordRejected)
	}
	if isRepositoryLockedOutput(result.output) {
		return ErrRepositoryBusy
	}
	return fmt.Errorf("%s failed (restic exit code %d)", action, result.exitCode)
}
