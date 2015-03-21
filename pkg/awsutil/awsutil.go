package awsutil

import (
	"crypto/md5"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"strings"
)

// https://tools.ietf.org/html/rfc4716#section-4
func FingerprintImportedKey(privateKey *rsa.PrivateKey) (string, error) {
	data, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", err
	}
	md5Data := md5.Sum(data)
	strbytes := make([]string, len(md5Data))
	for i, b := range md5Data {
		strbytes[i] = fmt.Sprintf("%02x", b)
	}
	return strings.Join(strbytes, ":"), nil
}
