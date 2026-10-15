package backup

import (
	"fmt"
	"strings"
)

type resticFailureCause uint8

const (
	resticFailureUnknown resticFailureCause = iota
	resticFailureAccessDenied
	resticFailureAccessKey
	resticFailureSecretKey
	resticFailureSignature
	resticFailureSessionToken
	resticFailureRegion
	resticFailureDNS
	resticFailureConnection
	resticFailureTimeout
	resticFailureTLS
	resticFailureMissingRepository
	resticFailureContention
)

var resticFailureRules = []struct {
	cause   resticFailureCause
	markers []string
}{
	{resticFailureSessionToken, []string{"expiredtoken", "invalidtoken", "missingauthenticationtoken", "security token included in the request is invalid", "session token is missing", "session token has expired", "provided token has expired"}},
	{resticFailureAccessKey, []string{"invalidaccesskeyid", "invalid access key", "access key id you provided does not exist"}},
	{resticFailureSecretKey, []string{"invalidsecretaccesskey", "invalid secret access key"}},
	{resticFailureSignature, []string{"signaturedoesnotmatch", "invalidsignature", "signature mismatch", "signature does not match", "request signature we calculated does not match"}},
	{resticFailureRegion, []string{"authorizationheadermalformed", "permanentredirect", "incorrectendpoint", "illegallocationconstraintexception", "bucketregionerror", "301 moved permanently", "307 temporary redirect", "authorization header is malformed", "must be addressed using the specified endpoint"}},
	{resticFailureAccessDenied, []string{"accessdenied", "access denied", "403 forbidden"}},
	{resticFailureDNS, []string{"no such host", "temporary failure in name resolution", "server misbehaving"}},
	{resticFailureTLS, []string{"x509:", "tls: handshake", "tls handshake", "certificate verify failed", "certificate has expired"}},
	{resticFailureTimeout, []string{"i/o timeout", "connection timed out", "context deadline exceeded", "timeout awaiting response headers"}},
	{resticFailureConnection, []string{"connection refused", "connection reset", "network is unreachable", "no route to host"}},
	{resticFailureMissingRepository, []string{"repository does not exist", "is there a repository at the following location?"}},
	{resticFailureContention, []string{"repository is already locked", "unable to create lock", "repository is locked"}},
}

func classifyResticFailure(output string) resticFailureCause {
	text := strings.ToLower(output)
	for _, rule := range resticFailureRules {
		for _, marker := range rule.markers {
			if strings.Contains(text, marker) {
				return rule.cause
			}
		}
	}
	return resticFailureUnknown
}

func resticFailure(action string, result bufferedResticResult) error {
	return resticCauseFailure(action, result.exitCode, classifyResticFailure(result.output))
}

func resticCauseFailure(action string, exitCode int, cause resticFailureCause) error {
	prefix := fmt.Sprintf("backup %s failed with restic exit code %d", action, exitCode)
	if exitCode == resticExitWrongPassword {
		return fmt.Errorf("%s: wrong password or no usable repository key found; check the backup password, missing keys or a non-S3 endpoint; repository existence has not been confirmed", prefix)
	}
	if exitCode == resticExitRepoDoesNotExist {
		cause = resticFailureMissingRepository
	}
	if exitCode == resticExitRepoLocked {
		cause = resticFailureContention
	}
	var explanation string
	switch cause {
	case resticFailureAccessDenied:
		explanation = "access denied; check bucket policy, identity permissions and repository access"
	case resticFailureAccessKey:
		explanation = "invalid access key reported; check the configured access key and its provider account"
	case resticFailureSecretKey:
		explanation = "invalid secret access key reported; check the configured secret key and matching access key"
	case resticFailureSignature:
		explanation = "request signature rejected; check the access key and secret key pairing, signing region and clock"
	case resticFailureSessionToken:
		explanation = "missing, invalid or expired session token reported; check whether the credentials require a session token"
	case resticFailureRegion:
		explanation = "region or endpoint redirect reported; check the bucket region and S3 API endpoint"
	case resticFailureDNS:
		explanation = "DNS resolution failed; check endpoint spelling and the agent host's DNS configuration"
	case resticFailureConnection:
		explanation = "connection failed; check endpoint availability, port, routing and firewall rules"
	case resticFailureTimeout:
		explanation = "connection timeout reported; check endpoint availability, routing and firewall rules"
	case resticFailureTLS:
		explanation = "TLS certificate or handshake failure; check the endpoint certificate, trust chain and TLS configuration"
	case resticFailureMissingRepository:
		explanation = "repository not found; check the repository location, storage availability and recorded backup history"
	case resticFailureContention:
		explanation = "repository locked; check for another active repository operation before trying again"
	default:
		explanation = "unrecognised failure"
	}
	return fmt.Errorf("%s: %s", prefix, explanation)
}
