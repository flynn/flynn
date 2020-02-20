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
		// go run generate_cert.go  --rsa-bits 2048 --host 127.0.0.1,::1,example.com,*.example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
		Cert: `-----BEGIN CERTIFICATE-----
MIIDVzCCAj+gAwIBAgIQc1f0KZWW0ci2JjRJLvVEkzANBgkqhkiG9w0BAQsFADAq
MRAwDgYDVQQKEwdBY21lIENvMRYwFAYDVQQDEw10ZXN0LmZseW5uLmlvMCAXDTcw
MDEwMTAwMDAwMFoYDzIwODQwMTI5MTYwMDAwWjAqMRAwDgYDVQQKEwdBY21lIENv
MRYwFAYDVQQDEw10ZXN0LmZseW5uLmlvMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAm4/aXFjZ2Z6zo+1vnMEIAdmMBvJM+rOZaMKV+T3Y6GmmNxWQ+1fr
ljHui+P0Yru9eX4jtgMmx8saArbptpNwkey3ogxfhHzhD2SafmabDUu91YOEGuDb
QF8GPw5aBcRDRzoCLBtvlLQD+JLYNzU27pfjOgtorpQI+Aaj2Q88vU3cd3tRvoXH
B9ftupuFdWVhlV6vE4Tm7hlL7u342Zf7v45feps/ZMCuesEY6B6ZBX1DUuuCI+uK
KI2lEqzS7i8ErE6CgpcCwiHOClxxA15K9MUMLApgQEAmE+QSTEoaYMs/4hnZlhTR
LnK4DrNZgZN6s/mszojY64sAT3RUMt6OFwIDAQABo3cwdTAOBgNVHQ8BAf8EBAMC
AqQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUwAwEB/zA9BgNVHREE
NjA0ggtleGFtcGxlLmNvbYINKi5leGFtcGxlLmNvbYcEfwAAAYcQAAAAAAAAAAAA
AAAAAAAAATANBgkqhkiG9w0BAQsFAAOCAQEAHbHVxz48sJC1JZZmtPpp5QIjv/y5
d1GFYuiTYxKvzcivrgmJTyRefCVS2hltYjt1ZVzqvRirhEGGcwgv5WY9XAi5cHZS
5/WUqDTt1z8ObmHctAeW60zaN027RhhLk+vwtkvYd6LH8L2et+30+CBD+B2KzYJA
SlZiAz5WZDTCepCAaYNiaDYW8fLHzqO9EZm9b9CpWS0Y92QmtPRriiMR4mjOhAys
2uZMI0n2iPtDvyfyLGCV0TbeVd8PCNB0FctDZkm682GhH7Jb6DIBe8TIiHin6l/i
nbRn21Wmm4WJmSuIf/pM4llr0IFJqZ9ADosiTc+PY+Iipfio93RwzKePFA==
-----END CERTIFICATE-----`,
		PrivateKey: `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCbj9pcWNnZnrOj
7W+cwQgB2YwG8kz6s5lowpX5PdjoaaY3FZD7V+uWMe6L4/Riu715fiO2AybHyxoC
tum2k3CR7LeiDF+EfOEPZJp+ZpsNS73Vg4Qa4NtAXwY/DloFxENHOgIsG2+UtAP4
ktg3NTbul+M6C2iulAj4BqPZDzy9Tdx3e1G+hccH1+26m4V1ZWGVXq8ThObuGUvu
7fjZl/u/jl96mz9kwK56wRjoHpkFfUNS64Ij64oojaUSrNLuLwSsToKClwLCIc4K
XHEDXkr0xQwsCmBAQCYT5BJMShpgyz/iGdmWFNEucrgOs1mBk3qz+azOiNjriwBP
dFQy3o4XAgMBAAECggEAc3/cPeKOZSCK+nRpATblDhKLAAFZHG7KwVhwZ6z+5pjC
m3V8vtEpjsul9OGcas4/wPvh4dr3KTJoLonGfxN7ai0arsuHA+SAaaBlgOIHz1hk
KypJpHc88s9a4Ohz+IIe/bxZLox0TTFXHXFR7bOqBH5rbIZaA/zPp++uKZRBob8M
NUEbfodLxUL5sDpX9aL1zQgyWBWf6a6g8q4xtMwPs5ylctk2G6X0ssyEWl9maGd+
aN8c5YiodzKqbhfSF3NS8Y30/wtvCDMWx623XnVupfYtFlXJaOVqm416XuESQDCZ
QNOto1J3pdVUBKxhYJGgyPITM2AfcoCRx45rvLdL2QKBgQDCDtw0fIbg19vBu1gL
jHInL3eoxaY0Mnvmklf8dZRm2aZElJg0gXHhnNB1y+ef3T/h4O8zNLenGez0tg+B
+4EtDOHjBCCdDDxk+EUHAwUPJaAz9Zxz4W17fB6jWUwnEPDBCXbIva32Yoa+GRJW
D3NELda0HmMttHhK4b1SkWWB9QKBgQDNN1gqFL2k5kSPRI6xdkjoe5QEqp+R+cLq
MT6B7Dr9K2od+/EjpGPTuo18UL+Gvrk+Z4YVAZkMJFAyLO0zJyVXRK0S6tVsRecD
4pYbPzr17zNkKnCjr0bQJuuM7NGCXCTsBxUU/F26EM+P3TRvPv4+1V/zvYI2aPwt
3s51dV5sWwKBgDyjph3kl8Ukzq/gGegp7/XcuFiNwpzm9Z6cNlBWcZQeCP2/LTyj
AnIMrXtRx0RGP9MWlch7fbQCvu/NAFWOwNPSBbgJryNEEo8+oVtKj0cna8Mwyb3Q
QIToyS4kFk7S1ViM24ho9TZbnV1Dul4YH927MS9Bm55JmZlUpvNpKb4NAoGBAMJE
Ghn7+GsZ8N0PMWWda/doxP6F5vjxTysT4vBrCIyRhKtNzUDIZhgRCc8dQbH06rfA
mJVaJd/woFpfXUyHSjoKsSyvUcplggOThDXW7aHTBvtTkb3iN07lCScnKE4XnHwz
WCm9nZx+PX8bEIAfSd+Bbov2YkXPrKpfuWJH8VLxAoGBAK5tpe2Kn2iIuOCp6H8X
HKge6XTuobkQOGwBjCuPiqw6sG3IUMe5jZzzW4IWPMwIkxjWiDzxiD4aHZARVLed
Q1oXP/yjdQTsw7YPm0KM2rEmHfMs9U8/YAe7q/2GME69/0W9Ad8NF4JbINjHnZy9
aG49O8p+TtFPq45qG/uV58R/
-----END PRIVATE KEY-----`,
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
