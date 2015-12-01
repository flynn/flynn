package signed

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/agl/ed25519"
)

// Verifier describes the verification interface. Implement this interface
// to add additional verifiers to go-tuf.
type Verifier interface {
	// Verify takes a key, message and signature, all as byte slices,
	// and determines whether the signature is valid for the given
	// key and message.
	Verify(key []byte, msg []byte, sig []byte) error
}

// Verifiers is used to map algorithm names to Verifier instances.
var Verifiers = map[string]Verifier{
	"ed25519": Ed25519Verifier{},
	//"rsa":     RSAVerifier{},
}

// RegisterVerifier provides a convenience function for init() functions
// to register additional verifiers or replace existing ones.
func RegisterVerifier(name string, v Verifier) {
	Verifiers[name] = v
}

// Ed25519Verifier is an implementation of a Verifier that verifies ed25519 signatures
type Ed25519Verifier struct{}

func (v Ed25519Verifier) Verify(key []byte, msg []byte, sig []byte) error {
	var sigBytes [ed25519.SignatureSize]byte
	if len(sig) != len(sigBytes) {
		return ErrInvalid
	}
	copy(sigBytes[:], sig)

	var keyBytes [ed25519.PublicKeySize]byte
	copy(keyBytes[:], key)

	if !ed25519.Verify(&keyBytes, msg, &sigBytes) {
		return ErrInvalid
	}
	return nil
}

// RSAVerifier is an implementation of a Verifier that verifies RSA signatures.
// N.B. Currently not covered by unit tests, use at your own risk.
type RSAVerifier struct{}

func (v RSAVerifier) Verify(key []byte, msg []byte, sig []byte) error {
	digest := sha256.Sum256(msg)
	pub, err := x509.ParsePKIXPublicKey(key)
	if err != nil {
		return ErrInvalid
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return ErrInvalid
	}

	if err = rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, digest[:], sig); err != nil {
		return ErrInvalid
	}
	return nil
}
