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

	pin, _ := hex.DecodeString("3be2ee0b13072aefd2a57ae4c6beb80d2bbdf2250e2e9db8c2153fd9905432c7")
	config := &Config{Pin: pin}
	conn, err := config.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()

	config.Pin[0] = 0
	conn, err = config.Dial("tcp", addr)
	if err != ErrPinFailure || conn != nil {
		t.Fatalf("Expected to get (nil, ErrPinFailure), got (%v, %v)", conn, err)
	}
}

// localhostCert is a PEM-encoded TLS cert with SAN IPs
// "127.0.0.1" and "[::1]", expiring at the last second of 2049 (the end
// of ASN.1 time).
// generated from src/crypto/tls:
// go run generate_cert.go --host 127.0.0.1,::1,example.com --ca --start-date "Jan 1 00:00:00 1970" --duration=1000000h
var localhostCert = []byte(`-----BEGIN CERTIFICATE-----
MIICFDCCAX2gAwIBAgIRAO+BRQ2pjCCN8Gou1SjbeDgwDQYJKoZIhvcNAQELBQAw
EjEQMA4GA1UEChMHQWNtZSBDbzAgFw03MDAxMDEwMDAwMDBaGA8yMDg0MDEyOTE2
MDAwMFowEjEQMA4GA1UEChMHQWNtZSBDbzCBnzANBgkqhkiG9w0BAQEFAAOBjQAw
gYkCgYEArt9hcoNRK3fbDW+3AlIjEEnJRsrBjj8K3bfRCXYoxkFnhIOFx0BpfxA5
qNp/AxYxVvP0vlGpnhbS3rRc3RATfp13dc/cFNoh47Xxw8KX7GjRmdSvNbuLicqA
eS4yyaz0z05tC2DIlyt97fYQaM9MTPE2Q3f7/ojOoEPnxlGCPE0CAwEAAaNoMGYw
DgYDVR0PAQH/BAQDAgKkMBMGA1UdJQQMMAoGCCsGAQUFBwMBMA8GA1UdEwEB/wQF
MAMBAf8wLgYDVR0RBCcwJYILZXhhbXBsZS5jb22HBH8AAAGHEAAAAAAAAAAAAAAA
AAAAAAEwDQYJKoZIhvcNAQELBQADgYEAji5Zyv9DK1SSoFsTNSv5GgsYScRa3ZFZ
UUVIrcVjAExWVy3jI98amLBq2CvASFlyXQkofWS5GD9TZe6XkCwGoUDRjf7d7gQV
YdAoDRK6B48ioFBsx+jxNWoU/4OhAwofLV3IfPDqLJkSpZkzINL3hHBQCKeIvDFH
5d7L4hGgcLU=
-----END CERTIFICATE-----`)

// localhostKey is the private key for localhostCert.
var localhostKey = []byte(`-----BEGIN PRIVATE KEY-----
MIICdwIBADANBgkqhkiG9w0BAQEFAASCAmEwggJdAgEAAoGBAK7fYXKDUSt32w1v
twJSIxBJyUbKwY4/Ct230Ql2KMZBZ4SDhcdAaX8QOajafwMWMVbz9L5RqZ4W0t60
XN0QE36dd3XP3BTaIeO18cPCl+xo0ZnUrzW7i4nKgHkuMsms9M9ObQtgyJcrfe32
EGjPTEzxNkN3+/6IzqBD58ZRgjxNAgMBAAECgYAaEm3p79ArRexf3XcQnoRhyk57
AoHHHnkVQ3GkEnzTyi6P4DgS0/SmoBmopiLnp+hlSWwE8BH04vw/fe6Weu4c5EB/
ebaBNY4E8EWVGvEd6tLHq2Gvc2rJYUxgh9jJNLQXYkz5lIXa0eejDWyULk3QbXKI
3r+guGxHqn+7EDGEFQJBAORtIfH5wssaNbgeI2huRb6GZfNyZJ8dcFk3pl53wqFa
eUzmr9twFKmhfvBK5K6hjOjE6LrdCGLKjD1ZlC9ikYsCQQDD+1J4/QvreHvdArlc
BF9FlsbUzzoKDsNSKk4TlInSouasBl+sc280RV2fyOFMPNkm415SG3A9M/F5P6Ix
TfSHAkEA2wzok4J+0YQF5dVJATlWOpnppKabZZa2iWf7a/YOt+rqDdve4mE9/1m2
QDqhx/F2DjXeNGwIQayZBbAkkbhFdwJBAKUstYa5Jwmvgx1zhUvzd2SMPknv2afO
Z3pho2pHP52ipC2KNap/o9L3P4BC6ve5NP/ck4s6Cu/aToN1STqqzBMCQDqFgAWs
vkKtlp9x5NBoVUpouvp/D7Cq+RT5pYgDuAfcePejUta/JNVK20M0UPnRq7wcexn2
wuRE6ChfxY7Tljg=
-----END PRIVATE KEY-----`)
