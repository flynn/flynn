package backend

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
)

func init() {
	Backends["azure"] = NewAzure
}

const azureMaxBlockSize = 4194304 // 4MiB

func NewAzure(name string, info map[string]string) (Backend, error) {
	b := &azureBackend{
		name:      name,
		container: info["container"],
	}
	accountName := info["account_name"]
	accountKey := info["account_key"]

	if b.container == "" {
		return nil, fmt.Errorf("blobstore: missing Azure Storage container param for %s", name)
	}
	if accountName == "" {
		return nil, fmt.Errorf("blobstore: missing Azure Storage account_name param for %s", name)
	}
	if accountKey == "" {
		return nil, fmt.Errorf("blobstore: missing Azure Storage account_key param for %s", name)
	}

	client, err := storage.NewBasicClient(accountName, accountKey)
	if err != nil {
		return nil, fmt.Errorf("blobstore: error creating Azure Storage client %s: %s", name, err)
	}
	b.client = client.GetBlobService()

	ok, err := b.client.ContainerExists(b.container)
	if err != nil {
		return nil, fmt.Errorf("blobstore: error checking if Azure Storage container %q exists for %s: %s", b.container, name, err)
	}
	if !ok {
		return nil, fmt.Errorf("blobstore: Azure Storage container %q does not exists for %s", b.container, name)
	}

	return b, nil
}

type azureBackend struct {
	name      string
	container string
	client    storage.BlobStorageClient
}

func (b *azureBackend) Name() string {
	return b.name
}

func (b *azureBackend) Put(tx *postgres.DBTx, info FileInfo, r io.Reader, appendBlob bool) error {
	if appendBlob {
		// TODO(titanous): This is a hack, we should modify the block list.
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

	// Create blob that will be filled with blocks
	if err := b.client.CreateBlockBlob(b.container, info.ExternalID); err != nil {
		return err
	}
	var blocks []storage.Block

	// Create blocks
	buf := make([]byte, azureMaxBlockSize)
	for {
		n, err := io.ReadFull(r, buf)
		if err == io.EOF {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			return err
		}
		data := buf[:n]
		md5sum := md5.Sum(data)
		blockID := base64.StdEncoding.EncodeToString(random.Bytes(16))

		if err := b.client.PutBlockWithLength(
			b.container,
			info.ExternalID,
			blockID,
			uint64(n),
			bytes.NewReader(data),
			map[string]string{"Content-MD5": base64.StdEncoding.EncodeToString(md5sum[:])},
		); err != nil {
			return err
		}
		blocks = append(blocks, storage.Block{ID: blockID, Status: storage.BlockStatusUncommitted})

		if err == io.ErrUnexpectedEOF {
			break
		}
	}

	// Save the list of blocks to the blob
	return b.client.PutBlockList(b.container, info.ExternalID, blocks)
}

func (b *azureBackend) Copy(tx *postgres.DBTx, dst, src FileInfo) error {
	// The Copy Blob operation is asynchronous, so we do the copy here instead.
	existing, err := b.Open(tx, src, false)
	if err != nil {
		return err
	}
	return b.Put(tx, dst, existing, false)
}

func (b *azureBackend) Delete(tx *postgres.DBTx, info FileInfo) error {
	return b.client.DeleteBlob(b.container, info.ExternalID, nil)
}

func (b *azureBackend) Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error) {
	if txControl {
		// We don't need the database transaction, so clean it up
		tx.Rollback()
	}

	url, err := b.client.GetBlobSASURI(b.container, info.ExternalID, time.Now().Add(10*time.Minute), "r")
	return newRedirectFileStream(url), err
}
