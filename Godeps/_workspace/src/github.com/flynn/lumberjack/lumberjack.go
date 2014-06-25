// Package lumberjack provides a rolling logger.
//
// Lumberjack is intended to be one part of a logging infrastructure.
// It is not an all-in-one solution, but instead is a pluggable
// component at the bottom of the logging stack that simply controls the files
// to which logs are written.
//
// Lumberjack plays well with any logger that can write to an io.Writer,
// including the standard library's log package.
//
// Lumberjack assumes that only one process is writing to the output files.
// Using the same lumberjack configuration from multiple processes on the same
// machine will result in improper behavior.
package lumberjack

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// Some helper constants to make your declarations easier to read.

	Megabyte = 1024 * 1024
	Gigabyte = 1024 * Megabyte

	defaultNameFormat = "2006-01-02T15-04-05.000000000.log"
	defaultMaxSize    = 100 * Megabyte
)

// ensure we always implement io.WriteCloser
var _ io.WriteCloser = (*Logger)(nil)

// Logger is an io.WriteCloser that writes to a log file in the given directory
// with the given NameFormat.  NameFormat should include a time formatting
// layout in it that produces a valid unique filename for the OS.  For more
// about time formatting layouts, read http://golang.org/pkg/time/#pkg-constants.
//
// The date encoded in the filename by NameFormat is used to determine which log
// files are most recent in several situations.
//
// Logger opens or creates a logfile on first Write.  It looks for files in the
// directory that match its name format, and if the one with the most recent
// NameFormat date is less than MaxSize, it will open and append to that file.
// If no such file exists, or the file is >= MaxSize, a new file is created
// using the current time with NameFormat to generate the filename.
//
// Whenever a write would cause the current log file exceed MaxSize, a new file
// is created using the current time.
//
// Cleaning Up Old Log Files
//
// Whenever a new file gets created, old log files may be deleted.  The log file
// directory is scanned for files that match NameFormat.  The most recent files
// according to their NameFormat date will be retained, up to a number equal to
// MaxBackups (or all of them if MaxBackups is 0).  Any files with a last
// modified time (based on FileInfo.ModTime) older than MaxAge days are deleted,
// regardless of MaxBackups.
//
// If MaxBackups and MaxAge are both 0, no old log files will be deleted.
type Logger struct {
	// Dir determines the directory in which to store log files.
	// It defaults to os.TempDir() if empty.
	Dir string `json:"dir" yaml:"dir"`

	// NameFormat is the time formatting layout used to generate filenames.
	// It defaults to "2006-01-02T15-04-05.000.log".
	NameFormat string `json:"nameformat" yaml:"nameformat"`

	// MaxSize is the maximum size in bytes of the log file before it gets
	// rolled. It defaults to 100 megabytes.
	MaxSize int64 `json:"maxsize" yaml:"maxsize"`

	// MaxAge is the maximum number of days to retain old log files based on
	// FileInfo.ModTime.  Note that a day is defined as 24 hours and may not
	// exactly correspond to calendar days due to daylight savings, leap
	// seconds, etc. The default is not to remove old log files based on age.
	MaxAge int `json:"maxage" yaml:"maxage"`

	// MaxBackups is the maximum number of old log files to retain.  The default
	// is to retain all old log files (though MaxAge may still cause them to get
	// deleted.)
	MaxBackups int `json:"maxbackups" yaml:"maxbackups"`

	// LocalTime determines if the time used for formatting the filename is the
	// computer's local time.  The default is to use UTC time.
	LocalTime bool `json:"localtime" yaml:"localtime"`

	size  int64
	file  *os.File
	files []os.FileInfo
	mu    sync.Mutex
}

var (
	// currentTime exists so it can be mocked out by tests.
	currentTime = time.Now

	// day is the units that MaxAge converts into.  It is a variable so we can
	// change it during tests.
	day = 24 * time.Hour
)

// Write implements io.Writer.  If a write would cause the log file to be larger
// than MaxSize, a new log file is created using the current time formatted with
// PathFormat.  If the length of the write is greater than MaxSize, an error is
// returned.
func (l *Logger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	writeLen := int64(len(p))
	if writeLen > l.max() {
		return 0, fmt.Errorf(
			"write length %d exceeds maximum file size %d", writeLen, l.max(),
		)
	}

	if l.file == nil {
		if err = l.openExistingOrNew(len(p)); err != nil {
			return 0, err
		}
	}

	if l.size+writeLen > l.max() {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = l.file.Write(p)
	l.size += int64(n)

	return n, err
}

// File returns the path to the current log file and its size.
func (l *Logger) File() (name string, size int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		name = l.file.Name()
	}
	size = l.size
	return
}

func (l *Logger) OldFiles() []os.FileInfo {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.files
}

// Close implements io.Closer, and closes the current logfile.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.close()
}

// close closes the file if it is open.
func (l *Logger) close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	l.files, _ = l.oldLogFiles()
	return err
}

// Rotate causes Logger to close the existing log file and immediately create a
// new one.  This is a helper function for applications that want to initiate
// rotations outside of the normal rotation rules, such as in response to
// SIGHUP.  After rotating, this initiates a cleanup of old log files according
// to the normal rules.
func (l *Logger) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rotate()
}

// rotate closes the current file, if any, opens a new file, and then calls
// cleanup.
func (l *Logger) rotate() error {
	if err := l.close(); err != nil {
		return nil
	}
	if err := l.openNew(); err != nil {
		return err
	}
	return l.cleanup()
}

// openNew opens a new log file for writing.
func (l *Logger) openNew() error {
	err := os.MkdirAll(l.dir(), 0744)
	if err != nil {
		return fmt.Errorf("can't make directories for new logfile: %s", err)
	}
	f, err := os.OpenFile(l.genFilename(), os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("can't open new logfile: %s", err)
	}
	info, err := f.Stat()
	if err != nil {
		// can't really do anything if close fails here
		_ = f.Close()
		return fmt.Errorf("can't get size of new logfile: %s", err)
	}
	l.size = info.Size()
	l.file = f
	return nil
}

// openExistingOrNew opens the most recently modified logfile in the log
// directory, if the current write would not put it over MaxSize.  If there is
// no such file or the write would put it over the MaxSize, a new file is
// created.
func (l *Logger) openExistingOrNew(writeLen int) error {
	if l.Dir == "" && l.NameFormat == "" {
		return l.openNew()
	}
	files, err := ioutil.ReadDir(l.dir())
	if os.IsNotExist(err) {
		return l.openNew()
	}
	if err != nil {
		return fmt.Errorf("can't read files in log file directory: %s", err)
	}
	sort.Sort(byFormatTime{files, l.format()})
	for _, info := range files {
		if info.IsDir() {
			continue
		}

		if !l.isLogFile(info) {
			continue
		}

		// the first file we find that matches our pattern will be the most
		// recently modified log file.
		if info.Size()+int64(writeLen) < l.max() {
			filename := filepath.Join(l.dir(), info.Name())
			file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				l.file = file
				return nil
			}
			// if we fail to open the old log file for some reason, just ignore
			// it and open a new log file.
		}
		break
	}
	return l.openNew()
}

// genFilename generates the name of the logfile from the current time.
func (l *Logger) genFilename() string {
	t := currentTime()
	if !l.LocalTime {
		t = t.UTC()
	}
	return filepath.Join(l.dir(), t.Format(l.format()))
}

// cleanup deletes old log files, keeping at most l.MaxBackups files, as long as
// none of them are older than MaxAge.
func (l *Logger) cleanup() error {
	if l.MaxBackups == 0 && l.MaxAge == 0 {
		return nil
	}

	var err error
	l.files, err = l.oldLogFiles()
	if err != nil {
		return err
	}

	var deletes []os.FileInfo

	if l.MaxBackups > 0 {
		deletes = l.files[l.MaxBackups:]
		l.files = l.files[:l.MaxBackups]
	}
	if l.MaxAge > 0 {
		diff := time.Duration(-1 * int64(day) * int64(l.MaxAge))

		cutoff := currentTime().Add(diff)

		for _, f := range l.files {
			if f.ModTime().Before(cutoff) {
				deletes = append(deletes, f)
			}
		}
	}

	if len(deletes) == 0 {
		return nil
	}

	go deleteAll(l.dir(), deletes)

	return nil
}

func deleteAll(dir string, files []os.FileInfo) {
	// remove files on a separate goroutine
	for _, f := range files {
		// what am I going to do, log this?
		_ = os.Remove(filepath.Join(dir, f.Name()))
	}
}

// oldLogFiles returns the list of backup log files stored in the same
// directory as the current log file, sorted by ModTime
func (l *Logger) oldLogFiles() ([]os.FileInfo, error) {
	files, err := ioutil.ReadDir(l.dir())
	if err != nil {
		return nil, fmt.Errorf("can't read log file directory: %s", err)
	}
	var logFiles []os.FileInfo

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if l.file != nil && filepath.Base(f.Name()) == filepath.Base(l.file.Name()) {
			continue
		}

		if l.isLogFile(f) {
			logFiles = append(logFiles, f)
		}
	}

	sort.Sort(byFormatTime{logFiles, l.format()})

	return logFiles, nil
}

// isLogFile reports where the name of f fits the format of a logfile created by
// this Logger.
func (l *Logger) isLogFile(f os.FileInfo) bool {
	_, err := time.Parse(l.format(), filepath.Base(f.Name()))
	return err == nil
}

// max returns the maximum size of log files before rolling..
func (l *Logger) max() int64 {
	if l.MaxSize == 0 {
		return defaultMaxSize
	}
	return l.MaxSize
}

// dir returns the directory in which log files should be stored.
func (l *Logger) dir() string {
	if l.Dir == "" {
		l.Dir = os.TempDir()
	}
	return l.Dir
}

// format returns the name format for log files.
func (l *Logger) format() string {
	if l.NameFormat != "" {
		return l.NameFormat
	}
	return defaultNameFormat
}

// byFormatTime sorts by newest time formatted in the name.
type byFormatTime struct {
	files  []os.FileInfo
	format string
}

func (b byFormatTime) Less(i, j int) bool {
	return b.time(i).After(b.time(j))
}

func (b byFormatTime) Swap(i, j int) {
	b.files[i], b.files[j] = b.files[j], b.files[i]
}

func (b byFormatTime) Len() int {
	return len(b.files)
}

func (b byFormatTime) time(i int) time.Time {
	t, err := time.Parse(b.format, filepath.Base(b.files[i].Name()))
	if err != nil {
		return time.Time{}
	}
	return t
}
