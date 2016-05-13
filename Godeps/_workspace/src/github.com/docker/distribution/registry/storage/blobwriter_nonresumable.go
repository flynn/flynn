// +build noresumabledigest

package storage

import (
	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/docker/distribution/context"
)

// resumeHashAt is a noop when resumable digest support is disabled.
func (bw *blobWriter) resumeDigestAt(ctx context.Context, offset int64) error {
	return errResumableDigestNotAvailable
}

// storeHashState is a noop when resumable digest support is disabled.
func (bw *blobWriter) storeHashState(ctx context.Context) error {
	return errResumableDigestNotAvailable
}
