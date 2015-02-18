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

type Params struct {
	Hosts []string
	IsCA  bool
	CA    *Certificate
}

type Certificate struct {
	Pin string
	PEM string
	DER []byte

	KeyPEM string
	Key    *rsa.PrivateKey
}

func Generate(p Params) (*Certificate, error) {
	var err error
	cert := &Certificate{}
	cert.Key, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(5 * 365 * 24 * time.Hour)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"Flynn"}},
		NotBefore:    notBefore,
		NotAfter:     notAfter,

		BasicConstraintsValid: true,
		IsCA: p.IsCA,
	}
	if p.IsCA {
		template.Subject.OrganizationalUnit = []string{"Flynn Ephemeral CA"}
		template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	} else {
		template.Subject.CommonName = p.Hosts[0]
		template.KeyUsage = x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}

	for _, host := range p.Hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	parent := template
	var parentKey interface{} = cert.Key
	if p.CA != nil {
		ca, err := x509.ParseCertificate(p.CA.DER)
		if err != nil {
			return nil, err
		}
		parent = ca
		parentKey = p.CA.Key
	}
	cert.DER, err = x509.CreateCertificate(rand.Reader, template, parent, &cert.Key.PublicKey, parentKey)
	if err != nil {
		return nil, err
	}

	h := sha256.New()
	h.Write(cert.DER)
	cert.Pin = base64.StdEncoding.EncodeToString(h.Sum(nil))

	var buf bytes.Buffer

	pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: cert.DER})
	cert.PEM = buf.String()

	buf.Reset()

	pem.Encode(&buf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(cert.Key)})
	cert.KeyPEM = buf.String()

	return cert, nil
}
