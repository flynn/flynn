package pinned

import (
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestPin(t *testing.T) {
	srv := httptest.NewUnstartedServer(nil)
	cert, err := tls.X509KeyPair(localhostCert, localhostKey)
	if err != nil {
		panic(fmt.Sprintf("NewTLSServer: %v", err))
	}
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	addr := srv.Listener.Addr().String()

	pin, _ := hex.DecodeString("a0d52b9f31f4522bb6abc5d560f973a704645126cb5a02092715487a401397fb")
	config := &Config{Pin: pin}
	conn, err := config.Dial("tcp", addr)
	if err != nil {
		t.Error(err)
	}
	conn.Close()

	config.Pin[0] = 0
	conn, err = config.Dial("tcp", addr)
	if err != ErrPinFailure || conn != nil {
		t.Errorf("Expected to get (nil, ErrPinFailure), got (%v, %v)", conn, err)
	}
}

// localhostCert is a PEM-encoded TLS cert with SAN IPs
// "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
// of ASN.1 time).
// generated from src/crypto/tls:
// go run generate_cert.go  --rsa-bits 512 --host 127.0.0.1,::1,example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIIBjjCCATigAwIBAgIQV9EDBiCDQuJLeAvSTlH6xjANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
AMNXtUafivNZh0oKlAPEhttrHumD1GnfmQQPjJmCptn1U9SIKVbaUjpAaHG1yL2s
DttYwR3fvzXcSZCDPUqoZ+kCAwEAAaNoMGYwDgYDVR0PAQH/BAQDAgKkMBMGA1Ud
JQQMMAoGCCsGAQUFBwMBMA8GA1UdEwEB/wQFMAMBAf8wLgYDVR0RBCcwJYILZXhh
bXBsZS5jb22HBH8AAAGHEAAAAAAAAAAAAAAAAAAAAAEwDQYJKoZIhvcNAQELBQAD
QQBOBhgaZhIMo004gBeneh27w4y48nJdVYjkB5wFo3b9KgDbG+IQcPGtbuZMA4jd
Y5PvEYtdY+WUr+h2R0WXBlFS
-----END CERTIFICATE-----`)

// localhostKey is the private key for localhostCert.
var localhostKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIBOwIBAAJBAMNXtUafivNZh0oKlAPEhttrHumD1GnfmQQPjJmCptn1U9SIKVba
UjpAaHG1yL2sDttYwR3fvzXcSZCDPUqoZ+kCAwEAAQJBAL2HAajN7uEBAlSCQu4M
5dNJ8aobcCJxAWOBOqdOrlhVDTl4IPQqMUZNlHIcB/9eZcqNMyguk9qlglHBNXzR
KgkCIQDz2dH/yDyd0IfuKiTbd6bgf6pu/IwILwCSV35JRy42EwIhAM0TMUj7s3rW
q4LCeKQt2KX78hiuWCKpg1L2C0LRupmTAiEAsHtZr7v0mubcKfNYX3n2TY44BEFE
+3tA96jY3iHlAP8CICC4tD958exivmER2KARtKTfa4SmpOd69rpRCgDyZ/zDAiAN
toS0gQWQj+zu3KyNKHOfjQUrNs3sdGSERZ7g0GBCRA==
-----END RSA PRIVATE KEY-----`)
