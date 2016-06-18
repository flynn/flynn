package ioutil

import (
	"fmt"
	"io"
)

type (
	SectionReader struct {
		src              io.ReadSeeker
		base, off, limit int64
	}
)

var errWhence = fmt.Errorf("Seek: invalid whence")

func NewSectionReader(src io.ReadSeeker, offset, length int64) *SectionReader {
	if sr, ok := src.(*SectionReader); ok {
		return &SectionReader{sr.src, sr.base + offset, sr.base + offset, sr.base + offset + length}
	}
	return &SectionReader{src, offset, offset, offset + length}
}

func (this *SectionReader) Offset() int64 {
	return this.base
}
func (this *SectionReader) Length() int64 {
	return this.limit - this.base
}
func (this *SectionReader) Read(p []byte) (n int, err error) {
	if this.off >= this.limit {
		return 0, io.EOF
	}
	_, err = this.src.Seek(this.off, 0)
	if err != nil {
		return
	}
	if max := this.limit - this.off; int64(len(p)) > max {
		p = p[:max]
	}
	n, err = this.src.Read(p)
	this.off += int64(n)
	return
}
func (this *SectionReader) Seek(offset int64, whence int) (n int64, err error) {
	switch whence {
	default:
		return 0, errWhence
	case 0:
		offset += this.base
	case 1:
		offset += this.off
	case 2:
		offset += this.limit
	}
	if offset < this.base || offset > this.limit {
		return 0, fmt.Errorf("Seek: invalid offset: %v, for: %v -> %v", offset, this.base, this.limit)
	}
	this.off = offset
	n, err = this.src.Seek(this.off, 0)
	n = this.off - this.base
	return
}
func (this *SectionReader) String() string {
	return fmt.Sprintf("{src: %v, base: %v, off: %v, limit: %v}", this.src, this.base, this.off, this.limit)
}
