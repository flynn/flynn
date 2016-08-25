// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"errors"
	"net/http"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/api/googleapi"
	raw "google.golang.org/api/storage/v1"
)

// Create creates the Bucket in the project.
// If attrs is nil the API defaults will be used.
func (b *BucketHandle) Create(ctx context.Context, projectID string, attrs *BucketAttrs) error {
	var bkt *raw.Bucket
	if attrs != nil {
		bkt = attrs.toRawBucket()
	} else {
		bkt = &raw.Bucket{}
	}
	bkt.Name = b.name
	req := b.c.raw.Buckets.Insert(projectID, bkt)
	_, err := req.Context(ctx).Do()
	return err
}

// Delete deletes the Bucket.
func (b *BucketHandle) Delete(ctx context.Context) error {
	req := b.c.raw.Buckets.Delete(b.name)
	return req.Context(ctx).Do()
}

// ACL returns an ACLHandle, which provides access to the bucket's access control list.
// This controls who can list, create or overwrite the objects in a bucket.
// This call does not perform any network operations.
func (c *BucketHandle) ACL() *ACLHandle {
	return c.acl
}

// DefaultObjectACL returns an ACLHandle, which provides access to the bucket's default object ACLs.
// These ACLs are applied to newly created objects in this bucket that do not have a defined ACL.
// This call does not perform any network operations.
func (c *BucketHandle) DefaultObjectACL() *ACLHandle {
	return c.defaultObjectACL
}

// Object returns an ObjectHandle, which provides operations on the named object.
// This call does not perform any network operations.
//
// name must consist entirely of valid UTF-8-encoded runes. The full specification
// for valid object names can be found at:
//   https://cloud.google.com/storage/docs/bucket-naming
func (b *BucketHandle) Object(name string) *ObjectHandle {
	return &ObjectHandle{
		c:      b.c,
		bucket: b.name,
		object: name,
		acl: &ACLHandle{
			c:      b.c,
			bucket: b.name,
			object: name,
		},
	}
}

// Attrs returns the metadata for the bucket.
func (b *BucketHandle) Attrs(ctx context.Context) (*BucketAttrs, error) {
	resp, err := b.c.raw.Buckets.Get(b.name).Projection("full").Context(ctx).Do()
	if e, ok := err.(*googleapi.Error); ok && e.Code == http.StatusNotFound {
		return nil, ErrBucketNotExist
	}
	if err != nil {
		return nil, err
	}
	return newBucket(resp), nil
}

// BucketAttrs represents the metadata for a Google Cloud Storage bucket.
type BucketAttrs struct {
	// Name is the name of the bucket.
	Name string

	// ACL is the list of access control rules on the bucket.
	ACL []ACLRule

	// DefaultObjectACL is the list of access controls to
	// apply to new objects when no object ACL is provided.
	DefaultObjectACL []ACLRule

	// Location is the location of the bucket. It defaults to "US".
	Location string

	// MetaGeneration is the metadata generation of the bucket.
	MetaGeneration int64

	// StorageClass is the storage class of the bucket. This defines
	// how objects in the bucket are stored and determines the SLA
	// and the cost of storage. Typical values are "STANDARD" and
	// "DURABLE_REDUCED_AVAILABILITY". Defaults to "STANDARD".
	StorageClass string

	// Created is the creation time of the bucket.
	Created time.Time
}

func newBucket(b *raw.Bucket) *BucketAttrs {
	if b == nil {
		return nil
	}
	bucket := &BucketAttrs{
		Name:           b.Name,
		Location:       b.Location,
		MetaGeneration: b.Metageneration,
		StorageClass:   b.StorageClass,
		Created:        convertTime(b.TimeCreated),
	}
	acl := make([]ACLRule, len(b.Acl))
	for i, rule := range b.Acl {
		acl[i] = ACLRule{
			Entity: ACLEntity(rule.Entity),
			Role:   ACLRole(rule.Role),
		}
	}
	bucket.ACL = acl
	objACL := make([]ACLRule, len(b.DefaultObjectAcl))
	for i, rule := range b.DefaultObjectAcl {
		objACL[i] = ACLRule{
			Entity: ACLEntity(rule.Entity),
			Role:   ACLRole(rule.Role),
		}
	}
	bucket.DefaultObjectACL = objACL
	return bucket
}

// toRawBucket copies the editable attribute from b to the raw library's Bucket type.
func (b *BucketAttrs) toRawBucket() *raw.Bucket {
	var acl []*raw.BucketAccessControl
	if len(b.ACL) > 0 {
		acl = make([]*raw.BucketAccessControl, len(b.ACL))
		for i, rule := range b.ACL {
			acl[i] = &raw.BucketAccessControl{
				Entity: string(rule.Entity),
				Role:   string(rule.Role),
			}
		}
	}
	dACL := toRawObjectACL(b.DefaultObjectACL)
	return &raw.Bucket{
		Name:             b.Name,
		DefaultObjectAcl: dACL,
		Location:         b.Location,
		StorageClass:     b.StorageClass,
		Acl:              acl,
	}
}

// ObjectList represents a list of objects returned from a bucket List call.
type ObjectList struct {
	// Results represent a list of object results.
	Results []*ObjectAttrs

	// Next is the continuation query to retrieve more
	// results with the same filtering criteria. If there
	// are no more results to retrieve, it is nil.
	Next *Query

	// Prefixes represents prefixes of objects
	// matching-but-not-listed up to and including
	// the requested delimiter.
	Prefixes []string
}

// List lists objects from the bucket. You can specify a query
// to filter the results. If q is nil, no filtering is applied.
//
// Deprecated. Use BucketHandle.Objects instead.
func (b *BucketHandle) List(ctx context.Context, q *Query) (*ObjectList, error) {
	it := b.Objects(ctx, q)
	attrs, pres, err := it.NextPage()
	if err != nil && err != Done {
		return nil, err
	}
	objects := &ObjectList{
		Results:  attrs,
		Prefixes: pres,
	}
	if it.NextPageToken() != "" {
		objects.Next = &it.query
	}
	return objects, nil
}

// Objects returns an iterator over the objects in the bucket that match the Query q.
// If q is nil, no filtering is done.
func (b *BucketHandle) Objects(ctx context.Context, q *Query) *ObjectIterator {
	it := &ObjectIterator{
		ctx:    ctx,
		bucket: b,
	}
	if q != nil {
		it.query = *q
	}
	return it
}

// An ObjectIterator is an iterator over ObjectAttrs.
type ObjectIterator struct {
	ctx      context.Context
	bucket   *BucketHandle
	query    Query
	pageSize int
	objs     []*ObjectAttrs
	prefixes []string
	err      error
}

// Next returns the next result. Its second return value is Done if there are
// no more results. Once Next returns Done, all subsequent calls will return
// Done.
//
// Internally, Next retrieves results in bulk. You can call SetPageSize as a
// performance hint to affect how many results are retrieved in a single RPC.
//
// SetPageToken should not be called when using Next.
//
// Next and NextPage should not be used with the same iterator.
//
// If Query.Delimiter is non-empty, Next returns an error. Use NextPage when using delimiters.
func (it *ObjectIterator) Next() (*ObjectAttrs, error) {
	if it.query.Delimiter != "" {
		return nil, errors.New("cannot use ObjectIterator.Next with a delimiter")
	}
	for len(it.objs) == 0 { // "for", not "if", to handle empty pages
		if it.err != nil {
			return nil, it.err
		}
		it.nextPage()
		if it.err != nil {
			it.objs = nil
			return nil, it.err
		}
		if it.query.Cursor == "" {
			it.err = Done
		}
	}
	o := it.objs[0]
	it.objs = it.objs[1:]
	return o, nil
}

// DefaultPageSize is the default page size for calls to an iterator's NextPage method.
const DefaultPageSize = 1000

// NextPage returns the next page of results, both objects (as *ObjectAttrs)
// and prefixes. Prefixes will be nil if query.Delimiter is empty.
//
// NextPage will return exactly the number of results (the total of objects and
// prefixes) specified by the last call to SetPageSize, unless there are not
// enough results available. If no page size was specified, or was set to less
// than 1, it uses DefaultPageSize.
//
// NextPage may return a second return value of Done along with the last page
// of results.
//
// After NextPage returns Done, all subsequent calls to NextPage will return
// (nil, Done).
//
// Next and NextPage should not be used with the same iterator.
func (it *ObjectIterator) NextPage() (objs []*ObjectAttrs, prefixes []string, err error) {
	defer it.SetPageSize(it.pageSize) // restore value at entry
	if it.pageSize <= 0 {
		it.pageSize = DefaultPageSize
	}
	for len(objs)+len(prefixes) < it.pageSize {
		it.pageSize -= len(objs) + len(prefixes)
		it.nextPage()
		if it.err != nil {
			return nil, nil, it.err
		}
		objs = append(objs, it.objs...)
		it.objs = nil
		prefixes = append(prefixes, it.prefixes...)
		if it.query.Cursor == "" {
			it.err = Done
			return objs, prefixes, it.err
		}
	}
	return objs, prefixes, nil
}

// nextPage gets the next page of results by making a single call to the underlying method.
// It sets it.objs, it.prefixes, it.query.Cursor, and it.err. It never sets it.err to Done.
func (it *ObjectIterator) nextPage() {
	if it.err != nil {
		return
	}
	req := it.bucket.c.raw.Objects.List(it.bucket.name)
	req.Projection("full")
	req.Delimiter(it.query.Delimiter)
	req.Prefix(it.query.Prefix)
	req.Versions(it.query.Versions)
	req.PageToken(it.query.Cursor)
	if it.pageSize > 0 {
		req.MaxResults(int64(it.pageSize))
	}
	resp, err := req.Context(it.ctx).Do()
	if err != nil {
		it.err = err
		return
	}
	it.query.Cursor = resp.NextPageToken
	for _, item := range resp.Items {
		it.objs = append(it.objs, newObject(item))
	}
	it.prefixes = resp.Prefixes
}

// SetPageSize sets the page size for all subsequent calls to NextPage.
// NextPage will return exactly this many items if they are present.
func (it *ObjectIterator) SetPageSize(pageSize int) {
	it.pageSize = pageSize
}

// SetPageToken sets the page token for the next call to NextPage, to resume
// the iteration from a previous point.
func (it *ObjectIterator) SetPageToken(t string) {
	it.query.Cursor = t
}

// NextPageToken returns a page token that can be used with SetPageToken to
// resume iteration from the next page. It returns the empty string if there
// are no more pages. For an example, see SetPageToken.
func (it *ObjectIterator) NextPageToken() string {
	return it.query.Cursor
}

// TODO(jbd): Add storage.buckets.update.

// Buckets returns an iterator over the buckets in the project. You may
// optionally set the iterator's Prefix field to restrict the list to buckets
// whose names begin with the prefix. By default, all buckets in the project
// are returned.
func (c *Client) Buckets(ctx context.Context, projectID string) *BucketIterator {
	return &BucketIterator{
		ctx:       ctx,
		client:    c,
		projectID: projectID,
	}
}

// A BucketIterator is an iterator over BucketAttrs.
type BucketIterator struct {
	// Prefix restricts the iterator to buckets whose names begin with it.
	Prefix string

	ctx       context.Context
	client    *Client
	projectID string
	pageSize  int
	pageToken string
	buckets   []*BucketAttrs
	err       error
}

// Next returns the next result. Its second return value is Done if there are
// no more results. Once Next returns Done, all subsequent calls will return
// Done.
//
// Internally, Next retrieves results in bulk. You can call SetPageSize as a
// performance hint to affect how many results are retrieved in a single RPC.
//
// SetPageToken should not be called when using Next.
//
// Next and NextPage should not be used with the same iterator.
func (it *BucketIterator) Next() (*BucketAttrs, error) {
	for len(it.buckets) == 0 { // "for", not "if", to handle empty pages
		if it.err != nil {
			return nil, it.err
		}
		it.nextPage()
		if it.err != nil {
			it.buckets = nil
			return nil, it.err
		}
		if it.pageToken == "" {
			it.err = Done
		}
	}
	b := it.buckets[0]
	it.buckets = it.buckets[1:]
	return b, nil
}

// NextPage returns the next page of results.
//
// NextPage will return exactly the number of results specified by the last
// call to SetPageSize, unless fewer results remain. If no page size was
// specified, or was set to less than 1, it uses DefaultPageSize.
//
// NextPage may return a second return value of Done along with the last page
// of results.
//
// After NextPage returns Done, all subsequent calls to NextPage will return
// (nil, Done).
//
// Next and NextPage should not be used with the same iterator.
func (it *BucketIterator) NextPage() (buckets []*BucketAttrs, err error) {
	defer it.SetPageSize(it.pageSize) // restore value at entry
	if it.pageSize <= 0 {
		it.pageSize = DefaultPageSize
	}
	for len(buckets) < it.pageSize {
		it.pageSize -= len(buckets)
		it.nextPage()
		if it.err != nil {
			return nil, it.err
		}
		buckets = append(buckets, it.buckets...)
		it.buckets = nil
		if it.pageToken == "" {
			it.err = Done
			return buckets, it.err
		}
	}
	return buckets, nil
}

// nextPage gets the next page of results by making a single call to the
// underlying method. It sets it.buckets, it.pageToken, and it.err. It never
// sets it.err to Done.
//
// Note that the underlying service is free to return less than pageSize items.
// It can even return none.
func (it *BucketIterator) nextPage() {
	if it.err != nil {
		return
	}
	req := it.client.raw.Buckets.List(it.projectID)
	req.Projection("full")
	req.Prefix(it.Prefix)
	req.PageToken(it.pageToken)
	if it.pageSize > 0 {
		req.MaxResults(int64(it.pageSize))
	}
	resp, err := req.Context(it.ctx).Do()
	if err != nil {
		it.err = err
		return
	}
	it.pageToken = resp.NextPageToken
	for _, item := range resp.Items {
		it.buckets = append(it.buckets, newBucket(item))
	}
}

// SetPageSize sets the page size for all subsequent calls to NextPage.
// NextPage will return exactly this many items if they are present.
func (it *BucketIterator) SetPageSize(pageSize int) {
	it.pageSize = pageSize
}

// SetPageToken sets the page token for the next call to NextPage, to resume
// the iteration from a previous point.
func (it *BucketIterator) SetPageToken(t string) {
	it.pageToken = t
}

// NextPageToken returns a page token that can be used with SetPageToken to
// resume iteration from the next page. It returns the empty string if there
// are no more pages. For an example, see SetPageToken.
func (it *BucketIterator) NextPageToken() string {
	return it.pageToken
}
