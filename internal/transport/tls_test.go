package transport

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestCertificateReuseAndAddressRenewal(t *testing.T) {
	dir := t.TempDir()
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.168.10.20"), net.ParseIP("100.101.102.103"), net.ParseIP("fd7a:115c:a1e0::1234")}
	first, err := Certificate(dir, ips)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(first.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range ips {
		if err := parsed.VerifyHostname(ip.String()); err != nil {
			t.Fatal(err)
		}
	}
	second, err := Certificate(dir, ips)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Certificate[0]) != string(second.Certificate[0]) {
		t.Fatal("certificate changed without an address change")
	}
	info, err := os.Stat(filepath.Join(dir, "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("private key is accessible to other users")
	}
	ips = append(ips, net.ParseIP("192.168.10.21"))
	renewed, err := Certificate(dir, ips)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(renewed.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = cert.VerifyHostname("192.168.10.21"); err != nil {
		t.Fatal(err)
	}
	if string(first.Certificate[0]) == string(renewed.Certificate[0]) {
		t.Fatal("certificate not renewed for new address")
	}
}
