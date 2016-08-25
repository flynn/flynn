package backend

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
)

func init() {
	Backends["gcs"] = NewGCS
}

func NewGCS(name string, info map[string]string) (Backend, error) {
	b := &gcsBackend{
		name:       name,
		bucketName: info["bucket"],
	}
	keyJSON := []byte(info["key"])

	if b.bucketName == "" {
		return nil, fmt.Errorf("blobstore: missing Google Cloud Storage bucket param for %s", name)
	}
	if len(keyJSON) == 0 {
		return nil, fmt.Errorf("blobstore: missing Google Cloud Storage key JSON param for %s", name)
	}

	jwtToken, err := google.JWTConfigFromJSON(keyJSON, "https://www.googleapis.com/auth/devstorage.read_write")
	if err != nil {
		return nil, fmt.Errorf("blobstore: error loading Google Cloud Storage JSON key: %s", err)
	}
	tokenSource := jwtToken.TokenSource(context.Background())

	// Test getting an OAuth token so we can disambiguate an issue with the
	// token and an issue with the bucket permissions below.
	if _, err := tokenSource.Token(); err != nil {
		return nil, fmt.Errorf("blobstore: error getting Google Cloud Storage OAuth token: %s", err)
	}

	pemBlock, _ := pem.Decode(jwtToken.PrivateKey)
	privateKey, err := x509.ParsePKCS8PrivateKey(pemBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("blobstore: error decoding Google Cloud Storage private key: %s", err)
	}
	rsaPrivateKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("blobstore: unexpected Google Cloud Storage key type: %T", privateKey)
	}
	b.signOpts = func() *storage.SignedURLOptions {
		return &storage.SignedURLOptions{
			GoogleAccessID: jwtToken.Email,
			SignBytes: func(b []byte) ([]byte, error) {
				digest := sha256.Sum256(b)
				return rsa.SignPKCS1v15(rand.Reader, rsaPrivateKey, crypto.SHA256, digest[:])
			},
			Method:  "GET",
			Expires: time.Now().Add(10 * time.Minute),
		}
	}

	client, err := storage.NewClient(context.Background(), option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("blobstore: error creating Google Cloud Storage client: %s", err)
	}
	b.bucket = client.Bucket(b.bucketName)

	_, err = b.bucket.Attrs(context.Background())
	if err != nil {
		return nil, fmt.Errorf("blobstore: error checking Google Cloud Storage bucket %q existence, ensure that it exists and Owner access for %s is included the bucket ACL: %q", b.bucketName, jwtToken.Email, err)
	}
	return b, nil
}

type gcsBackend struct {
	name   string
	bucket *storage.BucketHandle

	// parameters used for URL signing
	bucketName string
	signOpts   func() *storage.SignedURLOptions
}

func (b *gcsBackend) Name() string {
	return b.name
}

func (b *gcsBackend) Put(tx *postgres.DBTx, info FileInfo, r io.Reader, append bool) error {
	if append {
		// TODO(titanous): This is a hack, we should use resumable uploads.
		existing, err := b.Open(tx, info, false)
		if err != nil {
			return err
		}
		r = io.MultiReader(existing, r)
	}

	info.ExternalID = random.UUID()
	if err := tx.Exec("UPDATE files SET external_id = $2 WHERE file_id = $1", info.ID, info.ExternalID); err != nil {
		return err
	}

	w := b.bucket.Object(info.ExternalID).NewWriter(context.Background())
	w.ContentType = info.Type
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

func (b *gcsBackend) Copy(tx *postgres.DBTx, dst, src FileInfo) error {
	dst.ExternalID = random.UUID()
	if err := tx.Exec("UPDATE files SET external_id = $2 WHERE file_id = $1", dst.ID, dst.ExternalID); err != nil {
		return err
	}

	_, err := b.bucket.Object(src.ExternalID).CopyTo(context.Background(), b.bucket.Object(dst.ExternalID), nil)
	return err
}

func (b *gcsBackend) Delete(tx *postgres.DBTx, info FileInfo) error {
	return b.bucket.Object(info.ExternalID).Delete(context.Background())
}

func (b *gcsBackend) Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error) {
	if txControl {
		// We don't need the database transaction, so clean it up
		tx.Rollback()
	}

	url, err := storage.SignedURL(b.bucketName, info.ExternalID, b.signOpts())
	return newRedirectFileStream(url), err
}
