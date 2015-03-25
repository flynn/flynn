package ioutil

import (
	"fmt"
	"io"
)

type (
	MultiReadSeeker struct {
		components   []multiReadSeekerComponent
		offset, size int64
		initialized  bool
	}
	multiReadSeekerComponent struct {
		io.ReadSeeker
		offset, size int64
	}
)

func (this *MultiReadSeeker) Read(p []byte) (int, error) {
	// initialize
	err := this.init()
	if err != nil {
		return 0, err
	}

	for _, component := range this.components {
		// see where we're at in this component
		offset := this.offset - component.offset
		if offset >= component.size {
			continue
		}

		_, err = component.Seek(offset, 0)
		if err != nil {
			return 0, err
		}

		n, err := component.Read(p)
		this.offset += int64(n)
		if n > 0 && err == io.EOF {
			err = nil
		}
		return n, err
	}
	return 0, io.EOF
}

func (this *MultiReadSeeker) Seek(offset int64, whence int) (ret int64, err error) {
	// initialize
	err = this.init()
	if err != nil {
		return
	}

	switch whence {
	case 0:
	case 1:
		offset += this.offset
	case 2:
		offset += this.size
	default:
		err = fmt.Errorf("Seek: invalid whence")
		return
	}

	if offset > this.size || offset < 0 {
		err = fmt.Errorf("Seek: invalid offset")
		return
	}

	this.offset = offset
	ret = this.offset

	return
}

func (this *MultiReadSeeker) Size() (int64, error) {
	var err error
	if !this.initialized {
		err = this.init()
	}
	return this.size, err
}

func (this *MultiReadSeeker) init() error {
	if !this.initialized {
		for i, component := range this.components {
			size, err := component.Seek(0, 2)
			if err != nil {
				return err
			}
			this.components[i].offset = this.size
			this.components[i].size = size
			this.size += size
		}
		this.initialized = true
	}
	return nil
}

func NewMultiReadSeeker(readSeekers ...io.ReadSeeker) *MultiReadSeeker {
	components := make([]multiReadSeekerComponent, 0, len(readSeekers))
	for _, rdr := range readSeekers {
		components = append(components, multiReadSeekerComponent{rdr, 0, 0})
	}
	return &MultiReadSeeker{
		components: components,
	}
}
