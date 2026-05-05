package api

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"
)

// TLSCertInfo holds TLS certificate information.
type TLSCertInfo struct {
	Path         string  `json:"path"`
	Subject      string  `json:"subject"`
	Issuer       string  `json:"issuer"`
	NotBefore    string  `json:"notBefore"`
	NotAfter     string  `json:"notAfter"`
	DaysRemaining int   `json:"daysRemaining"`
	DNSNames     []string `json:"dnsNames,omitempty"`
	Valid        bool    `json:"valid"`
	Error        string  `json:"error,omitempty"`
}

// readTLSCert reads and parses a TLS certificate file.
func readTLSCert(path string) TLSCertInfo {
	info := TLSCertInfo{Path: path, Valid: false}
	if path == "" {
		info.Error = "no certificate path configured"
		return info
	}
	data, err := os.ReadFile(path)
	if err != nil {
		info.Error = fmt.Sprintf("read certificate: %v", err)
		return info
	}
	block, _ := pem.Decode(data)
	if block == nil {
		info.Error = "failed to decode PEM block"
		return info
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info.Error = fmt.Sprintf("parse certificate: %v", err)
		return info
	}
	now := time.Now()
	info.Subject = cert.Subject.String()
	info.Issuer = cert.Issuer.String()
	info.NotBefore = cert.NotBefore.Format(time.RFC3339)
	info.NotAfter = cert.NotAfter.Format(time.RFC3339)
	info.DaysRemaining = int(cert.NotAfter.Sub(now).Hours() / 24)
	info.DNSNames = cert.DNSNames
	info.Valid = now.Before(cert.NotAfter) && now.After(cert.NotBefore)
	return info
}
