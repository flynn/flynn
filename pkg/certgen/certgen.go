package certgen

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"time"
)

type Certificate struct {
	Lifespan time.Duration
	Hosts    []string
}

func Generate(options Certificate) (cert, privKey, pin string, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(options.Lifespan)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"Flynn"}},
		NotBefore:    notBefore,
		NotAfter:     notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	for _, host := range options.Hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return
	}

	h := sha256.New()
	h.Write(derBytes)
	pin = base64.StdEncoding.EncodeToString(h.Sum(nil))

	var buf bytes.Buffer

	pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	cert = buf.String()

	buf.Reset()

	pem.Encode(&buf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	privKey = buf.String()

	return
}
