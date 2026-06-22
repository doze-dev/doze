package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// BuildServerTLS returns a server tls.Config for client-facing TLS termination.
// If certFile and keyFile are given, it loads them. Otherwise it loads (or, on
// first use, generates and caches) a self-signed certificate under cacheDir —
// enough for local dev with sslmode=require, which encrypts without verifying
// the certificate.
func BuildServerTLS(certFile, keyFile, cacheDir string) (*tls.Config, error) {
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("loading TLS keypair: %w", err)
		}
		return serverConfig(cert), nil
	}

	crtPath := filepath.Join(cacheDir, "doze.crt")
	keyPath := filepath.Join(cacheDir, "doze.key")
	if fileExists(crtPath) && fileExists(keyPath) {
		if cert, err := tls.LoadX509KeyPair(crtPath, keyPath); err == nil {
			return serverConfig(cert), nil
		}
		// Fall through and regenerate if the cached pair is unreadable.
	}

	cert, err := generateSelfSigned(crtPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("generating self-signed certificate: %w", err)
	}
	return serverConfig(cert), nil
}

func serverConfig(cert tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
}

// generateSelfSigned creates a self-signed localhost certificate and caches it
// at crtPath/keyPath.
func generateSelfSigned(crtPath, keyPath string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost", Organization: []string{"doze"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(filepath.Dir(crtPath), 0o755); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(crtPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
