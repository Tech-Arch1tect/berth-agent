package ssl

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"berth-agent/internal/logging"

	"go.uber.org/zap"
)

const (
	CertFileName = "server.crt"
	KeyFileName  = "server.key"
	CertDir      = "./ssl"
)

type CertificateManager struct {
	certDir string
	logger  *logging.Logger
}

func NewCertificateManager(logger *logging.Logger) *CertificateManager {
	return &CertificateManager{
		certDir: CertDir,
		logger:  logger,
	}
}

func (cm *CertificateManager) EnsureCertificates() (string, string, error) {
	certPath := filepath.Join(cm.certDir, CertFileName)
	keyPath := filepath.Join(cm.certDir, KeyFileName)

	cm.logger.Debug("Checking for existing SSL certificates",
		zap.String("cert_path", certPath),
		zap.String("key_path", keyPath),
	)

	if cm.certificatesExist(certPath, keyPath) {
		cm.logger.Info("Loading existing SSL certificates",
			zap.String("cert_path", certPath),
			zap.String("key_path", keyPath),
		)
		return certPath, keyPath, nil
	}

	if err := cm.generateSelfSignedCertificate(certPath, keyPath); err != nil {
		cm.logger.Error("Failed to generate self-signed certificate",
			zap.Error(err),
			zap.String("cert_path", certPath),
			zap.String("key_path", keyPath),
		)
		return "", "", fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	return certPath, keyPath, nil
}

func (cm *CertificateManager) certificatesExist(certPath, keyPath string) bool {
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		cm.logger.Debug("Certificate file does not exist",
			zap.String("cert_path", certPath),
		)
		return false
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		cm.logger.Debug("Key file does not exist",
			zap.String("key_path", keyPath),
		)
		return false
	}
	cm.logger.Debug("Certificate and key files exist",
		zap.String("cert_path", certPath),
		zap.String("key_path", keyPath),
	)
	return true
}

func (cm *CertificateManager) generateSelfSignedCertificate(certPath, keyPath string) error {
	cm.logger.Info("Generating self-signed SSL certificate",
		zap.String("cert_path", certPath),
		zap.String("key_path", keyPath),
	)

	if err := os.MkdirAll(cm.certDir, 0755); err != nil {
		cm.logger.Error("Failed to create certificate directory",
			zap.Error(err),
			zap.String("cert_dir", cm.certDir),
		)
		return fmt.Errorf("failed to create certificate directory: %w", err)
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		cm.logger.Error("Failed to generate private key",
			zap.Error(err),
		)
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Berth Agent"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		DNSNames:    []string{"localhost", "berth-agent"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		cm.logger.Error("Failed to create certificate",
			zap.Error(err),
		)
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	certOut, err := os.Create(certPath)
	if err != nil {
		cm.logger.Error("Failed to open certificate file for writing",
			zap.Error(err),
			zap.String("cert_path", certPath),
		)
		return fmt.Errorf("failed to open cert.pem for writing: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		cm.logger.Error("Failed to write certificate",
			zap.Error(err),
			zap.String("cert_path", certPath),
		)
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	keyOut, err := os.Create(keyPath)
	if err != nil {
		cm.logger.Error("Failed to open key file for writing",
			zap.Error(err),
			zap.String("key_path", keyPath),
		)
		return fmt.Errorf("failed to open key.pem for writing: %w", err)
	}
	defer keyOut.Close()

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		cm.logger.Error("Failed to marshal private key",
			zap.Error(err),
		)
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}); err != nil {
		cm.logger.Error("Failed to write private key",
			zap.Error(err),
			zap.String("key_path", keyPath),
		)
		return fmt.Errorf("failed to write private key: %w", err)
	}

	cm.logger.Info("Successfully generated self-signed SSL certificate",
		zap.String("cert_path", certPath),
		zap.String("key_path", keyPath),
		zap.Time("valid_from", template.NotBefore),
		zap.Time("valid_until", template.NotAfter),
	)

	return nil
}
