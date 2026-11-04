package transport

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

// Certificate persists the key across launches and renews when interface
// addresses change. User supplied certificates bypass this helper.
func Certificate(dir string, ips []net.IP) (tls.Certificate, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return tls.Certificate{}, err
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if pair, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if cert, err := x509.ParseCertificate(pair.Certificate[0]); err == nil && time.Until(cert.NotAfter) > 24*time.Hour {
			valid := true
			for _, ip := range ips {
				if cert.VerifyHostname(ip.String()) != nil {
					valid = false
				}
			}
			if valid {
				return pair, nil
			}
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "RMic local connection"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: ips, DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	// Replace the private key atomically, preserving restrictive permissions even
	// if a pre-existing file was created with permissive permissions.
	for path, data := range map[string][]byte{keyPath: keyPEM, certPath: certPEM} {
		tmp, err := os.CreateTemp(dir, ".tls-*")
		if err != nil {
			return tls.Certificate{}, err
		}
		name := tmp.Name()
		_, writeErr := tmp.Write(data)
		closeErr := tmp.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(name)
			return tls.Certificate{}, fmt.Errorf("write certificate: %v %v", writeErr, closeErr)
		}
		if err := os.Rename(name, path); err != nil {
			_ = os.Remove(name)
			return tls.Certificate{}, err
		}
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}
