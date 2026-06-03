package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

type Authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func New() (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "hive-internal-ca",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		return nil, err
	}
	return &Authority{cert: cert, key: key}, nil
}

func (a *Authority) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw})
}

func (a *Authority) SignAgentCSR(csr *x509.CertificateRequest, nodeID string, ttl time.Duration) ([]byte, error) {
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: fmt.Sprintf("agent-%s", nodeID),
		},
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(ttl),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	raw, err := x509.CreateCertificate(rand.Reader, tpl, a.cert, csr.PublicKey, a.key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), nil
}
