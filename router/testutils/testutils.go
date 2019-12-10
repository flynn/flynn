package testutils

import (
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/flynn/flynn/pkg/tlscert"
)

var tlsCerts = map[string]*tlscert.Cert{
	"example.com": {
		// borrowed from net/http/httptest/server.go
		// PEM-encoded TLS cert with SAN IPs
		// "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
		// of ASN.1 time).
		// generated from src/pkg/crypto/tls:
		// go run generate_cert.go  --rsa-bits 1024 --host 127.0.0.1,::1,example.com,*.example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
		Cert: `-----BEGIN CERTIFICATE-----
MIICIjCCAYugAwIBAgIQep/lMfaW5CXuQ80O9YxUYTANBgkqhkiG9w0BAQsFADAS
MRAwDgYDVQQKEwdBY21lIENvMCAXDTcwMDEwMTAwMDAwMFoYDzIwODQwMTI5MTYw
MDAwWjASMRAwDgYDVQQKEwdBY21lIENvMIGfMA0GCSqGSIb3DQEBAQUAA4GNADCB
iQKBgQCzKVTTfyMlJ61uoVvYhPCd+16S1vEGmOCGjcY9Luj9WR+BadFL6bYyC97O
wSiAiCTO9KMlIUng7Pgqn86JH0jyKxcd70R2e/VjaUdtF0Ktt2f/ms2n+wigBsK0
qQEvTSseqrBdsgI7PMF5Ayr4n7xhiu+fWR4E8rJLSIZvkO2amwIDAQABo3cwdTAO
BgNVHQ8BAf8EBAMCAqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUw
AwEB/zA9BgNVHREENjA0ggtleGFtcGxlLmNvbYINKi5leGFtcGxlLmNvbYcEfwAA
AYcQAAAAAAAAAAAAAAAAAAAAATANBgkqhkiG9w0BAQsFAAOBgQA3P7dK8y7yx9P6
8YjkxNFRYIpzC7azf9J/Y1vw36h7MytYeV9PtWPQ+FhndjqheVLodORa5qfG5MED
5vkf/XtcZZcQAQtWiw6kYzJTVOitJtYXNsNLwaWVPb4ou03ZJO7dAbYOTu/5Jyhv
zKV/QKdrvRRxJ1JleBxLFHZYCiFJKw==
-----END CERTIFICATE-----`,
		PrivateKey: `-----BEGIN RSA PRIVATE KEY-----
MIICXQIBAAKBgQCzKVTTfyMlJ61uoVvYhPCd+16S1vEGmOCGjcY9Luj9WR+BadFL
6bYyC97OwSiAiCTO9KMlIUng7Pgqn86JH0jyKxcd70R2e/VjaUdtF0Ktt2f/ms2n
+wigBsK0qQEvTSseqrBdsgI7PMF5Ayr4n7xhiu+fWR4E8rJLSIZvkO2amwIDAQAB
AoGAXQpPxO23YKo0RMmDGvQeyMwrlvIMhTKLFxU1J7zevgK0e85qJJQgS+kiMhjZ
YbZR9y/QMY4SAb7OOcR3y3n1tP7/yWOvft9HhdCs34+adWeesllFO1Hhf9uzAAhF
oltOojyexBKwqq061+vxbmhsghRLmuhqq4L/w4K41MgKkZECQQDVkKD2sgyQl5Zk
kaW9nmBiwLRdmOTbXwgJPfQX3KYH95bCZ33SuOAzpZ6v2Zj6qVOGz9bf44KCF7iw
Ae4IaxqnAkEA1sK0m1qgkU+2LjIVbH3T2gAz77+zvZX+XoeJjq2Uj6VAEFqPjryQ
zYYkvbo2COnBe3vfP+o4rsUoYg556h5i7QJAevJ9SChuhVtPcGxM72Ha+V8ZNv0L
W6NU/AUXnhkf2FxYBWkRDZvzLqh9N51crYmHlYfXmyLeAkjnwSQLRftq5wJBAIYP
OMqZgg3zYlfn77OvwCUfZ0xLsJmyHf1IQjgMZuZcU2diAKcrUoDZMeo1aTGbKao5
oxy0yvleHV1IiBX7LekCQQCPd3SGvP4hx93wGunyLB/Yhod7NSZ8Y8nOPjf0Gh3j
1LvU3Jj5GMFFN+EwDoV/OWEFWx33//ei9uZekozxDSma
-----END RSA PRIVATE KEY-----`,
	},
}

var tlsCertsMux sync.Mutex

func TLSConfigForDomain(domain string) *tlscert.Cert {
	tlsCertsMux.Lock()
	defer tlsCertsMux.Unlock()
	domain = normalizeDomain(domain)
	parts := strings.SplitAfter(domain, ".")
	wildcard := "*."
	for i := 1; i < len(parts); i++ {
		wildcard += parts[i]
	}
	if c, ok := tlsCerts[domain]; ok {
		return c
	}
	if c, ok := tlsCerts[wildcard]; ok {
		return c
	}
	c, err := tlscert.Generate([]string{domain})
	if err != nil {
		panic(err)
	}
	tlsCerts[domain] = c
	return c
}

func RefreshTLSConfigForDomain(domain string) *tlscert.Cert {
	tlsCertsMux.Lock()
	defer tlsCertsMux.Unlock()
	domain = normalizeDomain(domain)
	c, err := tlscert.Generate([]string{domain})
	if err != nil {
		panic(err)
	}
	tlsCerts[domain] = c
	return c
}

func normalizeDomain(domain string) string {
	domain = strings.ToLower(domain)
	if d, _, err := net.SplitHostPort(domain); err == nil {
		domain = d
	}
	uri, err := url.Parse("http://" + domain)
	if err != nil {
		panic(err)
	}
	domain = uri.Host
	if strings.HasSuffix(domain, ".example.com") {
		domain = "example.com"
	}
	return domain
}
