package backend

import (
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

func init() {
	Backends["s3"] = NewS3
}

func NewS3(name string, info map[string]string) (Backend, error) {
	c := aws.NewConfig()
	b := &s3Backend{
		name:   name,
		bucket: info["bucket"],
	}

	if r := info["region"]; r != "" {
		c.WithRegion(r)
	}
	ec2Role := info["ec2_role"] == "true"
	id := info["access_key_id"]
	key := info["secret_access_key"]

	if b.bucket == "" {
		return nil, fmt.Errorf("blobstore: missing S3 bucket param for %s", name)
	}
	if !ec2Role && (id == "" || key == "") {
		return nil, fmt.Errorf("blobstore: missing S3 auth params for %s", name)
	}
	if ec2Role {
		c.WithCredentials(credentials.NewCredentials(&ec2rolecreds.EC2RoleProvider{
			Client:       ec2metadata.New(session.New(c.Copy())),
			ExpiryWindow: 5 * time.Minute,
		}))
	} else {
		c.WithCredentials(credentials.NewStaticCredentials(id, key, ""))
	}

	b.client = s3.New(session.New(c))

	_, err := b.client.HeadBucket(&s3.HeadBucketInput{Bucket: &b.bucket})
	if err != nil {
		return nil, fmt.Errorf("blobstore: error checking S3 bucket %q existence, ensure that it exists and these actions are authorized: s3:ListBucket, s3:PutObject, s3:GetObject, s3:DeleteObject, s3:ListMultipartUploadParts, s3:AbortMultipartUpload, s3:ListBucketMultipartUploads; %s", b.bucket, err)
	}
	return b, nil
}

type s3Backend struct {
	name   string
	bucket string
	client s3iface.S3API
}

func (b *s3Backend) Name() string {
	return b.name
}

func (b *s3Backend) Put(tx *postgres.DBTx, info FileInfo, r io.Reader, append bool) error {
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

	u := s3manager.NewUploaderWithClient(b.client)
	_, err := u.Upload(&s3manager.UploadInput{
		Bucket:      &b.bucket,
		Key:         &info.ExternalID,
		ContentType: &info.Type,
		Body:        r,
	})

	return err
}

func (b *s3Backend) Copy(tx *postgres.DBTx, dst, src FileInfo) error {
	dst.ExternalID = random.UUID()
	if err := tx.Exec("UPDATE files SET external_id = $2 WHERE file_id = $1", dst.ID, dst.ExternalID); err != nil {
		return err
	}

	_, err := b.client.CopyObject(&s3.CopyObjectInput{
		Bucket:      &b.bucket,
		CopySource:  aws.String(fmt.Sprintf("%s/%s", b.bucket, src.ExternalID)),
		Key:         &dst.ExternalID,
		ContentType: &dst.Type,
	})

	return err
}

func (b *s3Backend) Delete(tx *postgres.DBTx, info FileInfo) error {
	_, err := b.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: &b.bucket,
		Key:    &info.ExternalID,
	})
	return err
}

func (b *s3Backend) Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error) {
	if txControl {
		// We don't need the database transaction, so clean it up
		tx.Rollback()
	}

	req, _ := b.client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: &b.bucket,
		Key:    &info.ExternalID,
	})
	url, err := req.Presign(10 * time.Minute)
	if err != nil {
		return nil, err
	}

	return newRedirectFileStream(url), nil
}
