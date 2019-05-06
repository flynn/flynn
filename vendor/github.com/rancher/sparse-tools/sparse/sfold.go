package sparse

import (
	"os"
	"syscall"

	log "github.com/sirupsen/logrus"
)

const (
	batchBlockCount = 32
)

// FoldFile folds child snapshot data into its parent
func FoldFile(childFileName, parentFileName string) error {

	childFInfo, err := os.Stat(childFileName)
	if err != nil {
		panic("os.Stat(childFileName) failed, error: " + err.Error())
	}
	parentFInfo, err := os.Stat(parentFileName)
	if err != nil {
		panic("os.Stat(parentFileName) failed, error: " + err.Error())
	}

	// ensure no directory
	if childFInfo.IsDir() || parentFInfo.IsDir() {
		panic("at least one file is directory, not a normal file")
	}

	// ensure file sizes are equal
	if childFInfo.Size() != parentFInfo.Size() {
		panic("file sizes are not equal")
	}

	// open child and parent files
	childFileIo, err := NewDirectFileIoProcessor(childFileName, os.O_RDONLY, 0)
	if err != nil {
		panic("Failed to open childFile, error: " + err.Error())
	}
	defer childFileIo.Close()

	parentFileIo, err := NewDirectFileIoProcessor(parentFileName, os.O_WRONLY, 0)
	if err != nil {
		panic("Failed to open parentFile, error: " + err.Error())
	}
	defer parentFileIo.Close()

	return coalesce(parentFileIo, childFileIo)
}

func coalesce(parentFileIo FileIoProcessor, childFileIo FileIoProcessor) error {
	blockSize, err := getFileSystemBlockSize(childFileIo)
	if err != nil {
		panic("can't get FS block size, error: " + err.Error())
	}
	exts, err := GetFiemapExtents(childFileIo)
	if err != nil {
		log.Errorf("Failed to GetFiemapExtents of childFile filename: %s, err: %v", childFileIo.Name(), err)
		return err
	}
	for _, e := range exts {
		dataBegin := int64(e.Logical)
		dataEnd := int64(e.Logical + e.Length)

		// now we have a data start offset and length(hole - data)
		// let's read from child and write to parent file. We read/write up to
		// 32 blocks in a batch
		_, err = parentFileIo.Seek(dataBegin, os.SEEK_SET)
		if err != nil {
			log.Errorf("Failed to os.Seek os.SEEK_SET parentFile filename: %v, at: %v", parentFileIo.Name(), dataBegin)
			return err
		}

		batch := batchBlockCount * blockSize
		buffer := AllocateAligned(batch)
		for offset := dataBegin; offset < dataEnd; {
			size := batch
			if offset+int64(size) > dataEnd {
				size = int(dataEnd - offset)
			}
			// read a batch from child
			n, err := childFileIo.ReadAt(buffer[:size], offset)
			if err != nil {
				log.Errorf("Failed to read childFile filename: %v, size: %v, at: %v", childFileIo.Name(), size, offset)
				return err
			}
			// write a batch to parent
			n, err = parentFileIo.WriteAt(buffer[:size], offset)
			if err != nil {
				log.Errorf("Failed to write to parentFile filename: %v, size: %v, at: %v", parentFileIo.Name(), size, offset)
				return err
			}
			offset += int64(n)
		}
	}

	return nil
}

// get the file system block size
func getFileSystemBlockSize(fileIo FileIoProcessor) (int, error) {
	var stat syscall.Stat_t
	err := syscall.Stat(fileIo.Name(), &stat)
	return int(stat.Blksize), err
}
