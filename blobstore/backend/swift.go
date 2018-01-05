package backend

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/flynn/flynn/pkg/postgres"
	"github.com/flynn/flynn/pkg/random"
	"github.com/ncw/swift"
)

func init() {
	Backends["swift"] = NewSwift
}

func NewSwift(name string, info map[string]string) (Backend, error) {
	if info["username"] == "" {
		return nil, fmt.Errorf("blobstore: missing swift param username for %s", name)
	}

	if info["password"] == "" {
		return nil, fmt.Errorf("blobstore: missing swift param password for %s", name)
	}

	if info["auth_url"] == "" {
		return nil, fmt.Errorf("blobstore: missing swift param auth_url for %s", name)
	}

	if info["container"] == "" {
		return nil, fmt.Errorf("blobstore: missing swift param container for %s", name)
	}

	auth_version, err := strconv.Atoi(info["auth_version"])
	if err != nil {
		auth_version = 0
	}

	b := &swiftBackend{
		name:      name,
		container: info["container"],
		connection: &swift.Connection{
			Domain:         info["domain"],
			DomainId:       info["domain_id"],
			UserName:       info["username"],
			ApiKey:         info["password"],
			AuthUrl:        info["auth_url"],
			AuthVersion:    auth_version,
			Region:         info["region"],
			Tenant:         info["tenant"],
			TenantId:       info["tenant_id"],
			TenantDomain:   info["tenant_domain"],
			TenantDomainId: info["tenant_domain_id"],
			TrustId:        info["trust_id"],
		},
	}

	if err := b.connection.Authenticate(); err != nil {
		return nil, fmt.Errorf("blobstore: error connecting to swift: %s", err)
	}

	// To generate url for downloading object, account temp-url-key metadata is mandatory
	// https://docs.openstack.org/kilo/config-reference/content/object-storage-tempurl.html
	_, headers, _ := b.connection.Account()
	accountMeta := headers.AccountMetadata()

	if accountMeta["temp-url-key"] == "" {
		return nil, fmt.Errorf("blobstore: swift account temp-url-key metadata is not set")
	}

	b.tempUrlKey = accountMeta["temp-url-key"]

	if _, _, err := b.connection.Container(info["container"]); err != nil {
		return nil, fmt.Errorf("blobstore: swift container error: %s", err)
	}

	return b, nil
}

type swiftBackend struct {
	name       string
	connection *swift.Connection
	container  string
	tempUrlKey string
}

func (b *swiftBackend) Name() string {
	return b.name
}

func (b *swiftBackend) Put(tx *postgres.DBTx, info FileInfo, r io.Reader, append bool) error {
	if append {
		// This is a hack: there's no append support currently in Swift.
		// https://blueprints.launchpad.net/swift/+spec/object-append
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

	_, err := b.connection.ObjectPut(b.container, info.ExternalID, r, true, "", info.Type, nil)
	return err
}

func (b *swiftBackend) Copy(tx *postgres.DBTx, dst, src FileInfo) error {
	dst.ExternalID = random.UUID()
	if err := tx.Exec("UPDATE files SET external_id = $2 WHERE file_id = $1", dst.ID, dst.ExternalID); err != nil {
		return err
	}

	_, err := b.connection.ObjectCopy(b.container, src.ExternalID, b.container, dst.ExternalID, nil)
	return err
}

func (b *swiftBackend) Delete(tx *postgres.DBTx, info FileInfo) error {
	return b.connection.ObjectDelete(b.container, info.ExternalID)
}

func (b *swiftBackend) Open(tx *postgres.DBTx, info FileInfo, txControl bool) (FileStream, error) {
	if txControl {
		tx.Rollback()
	}

	url := b.connection.ObjectTempUrl(b.container, info.ExternalID, b.tempUrlKey, "GET", time.Now().Add(10*time.Minute))
	return newRedirectFileStream(url), nil
}
