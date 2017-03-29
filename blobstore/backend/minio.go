package backend

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/minio/minio-go"
)

func init() {
	Backends["minio"] = NewMinio
}

func NewMinio(name string, info map[string]string) (Backend, error) {
	endpoint := info["endpoint"]
	accessKeyID := info["access_key_id"]
	secretAccessKey := info["secret_access_key"]

	useHTTPS := true
	if strings.ToLower(info["insecure"]) == "true" {
		useHTTPS = false
	}
	if endpoint == "" {
		return nil, fmt.Errorf("blobstore: missing minio param endpoint for %s", name)
	}

	if accessKeyID == "" {
		return nil, fmt.Errorf("blobstore: missing minio param access_key_id for %s", name)
	}

	if secretAccessKey == "" {
		return nil, fmt.Errorf("blobstore: missing minio param secret_access_key for %s", name)
	}

	b := &minioBackend{
		name:     name,
		bucket:   info["bucket"],
		location: info["location"],
	}

	if b.bucket == "" {
		return nil, fmt.Errorf("blobstore: missing minio bucket param for %s", name)
	}
	if b.location == "" {
		b.location = "us-east-1"
	}
	// Initialize minio client object.
	var err error
	b.client, err = minio.New(endpoint, accessKeyID, secretAccessKey, useHTTPS)
	if err != nil {
		return nil, err
	}
	if err = b.checkBucket(); err != nil {
		return nil, err
	}
	return b, nil
}

type minioBackend struct {
	location string
	name     string
	bucket   string
	client   *minio.Client
}

func (b *minioBackend) checkBucket() error {
	err := b.client.MakeBucket(b.bucket, b.location)
	if err != nil {
		// Check to see if we already own this bucket (which happens if you run this twice)
		exists, err := b.client.BucketExists(b.bucket)
		if err == nil && exists {
			return nil
		}
		return err
	}
	return nil
}

func (b *minioBackend) Name() string {
	return b.name
}

func (b *minioBackend) Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error) {
	if txControl {
		// We don't need the database transaction, so clean it up
		tx.Rollback()
	}

	url, err := b.client.PresignedGetObject(b.bucket, info.ExternalID, 10*time.Minute, nil)
	if err != nil {
		return nil, err
	}

	return newRedirectFileStream(url.String()), nil
}

func (b *minioBackend) Put(tx *postgres.DBTx, info FileInfo, r io.Reader, append bool) error {
	if append {
		// This is a hack, the next easiest thing to do if we need to handle
		// upload resumption is to finalize the multipart upload when the client
		// disconnects and when the rest of the data arrives, start a new
		// multi-part upload copying the existing object as the first part
		// (which is supported by S3 as a specific API call). This requires
		// replacing the simple uploader, so it was not done in the first pass.
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
	_, err := b.client.PutObject(
		b.bucket,
		info.ExternalID,
		r,
		info.Type,
	)

	return err
}

func (b *minioBackend) Copy(tx *postgres.DBTx, dst, src FileInfo) error {
	dst.ExternalID = random.UUID()
	if err := tx.Exec("UPDATE files SET external_id = $2 WHERE file_id = $1", dst.ID, dst.ExternalID); err != nil {
		return err
	}
	copyConds := minio.CopyConditions{}
	return b.client.CopyObject(
		b.bucket,
		dst.ExternalID,
		fmt.Sprintf("%s/%s", b.bucket, src.ExternalID),
		copyConds,
	)
}

func (b *minioBackend) Delete(tx *postgres.DBTx, info FileInfo) error {
	return b.client.RemoveObject(
		b.bucket,
		info.ExternalID,
	)
}
