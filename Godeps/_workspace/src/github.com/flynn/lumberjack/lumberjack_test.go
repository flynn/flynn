package lumberjack

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/BurntSushi/toml"
	"gopkg.in/yaml.v1"
)

// !!!NOTE!!!
//
// Running these tests in parallel will almost certainly cause sporadic (or even
// regular) failures, because they're all messing with the same global variable
// that controls the logic's mocked time.Now.  So... don't do that.

// make sure we set the format to something safe for windows, too.
const format = "2006-01-02T15-04-05.000"

// Since all the tests uses the time to determine filenames etc, we need to
// control the wall clock as much as possible, which means having a wall clock
// that doesn't change unless we want it to.
var fakeCurrentTime = time.Now()

func fakeTime() time.Time {
	return fakeCurrentTime
}

func TestNewFile(t *testing.T) {
	currentTime = fakeTime

	dir := makeTempDir("TestNewFile", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Dir:        dir,
		NameFormat: format,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithLen(logFile(dir), n, t)
	fileCount(dir, 1, t)
}

func TestOpenExisting(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestOpenExisting", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	data := []byte("foo!")
	err := ioutil.WriteFile(filename, data, 0644)
	isNil(err, t)
	existsWithLen(filename, len(data), t)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	// make sure the file got appended
	existsWithLen(filename, len(data)+n, t)

	// make sure no other files were created
	fileCount(dir, 1, t)
}

func TestWriteTooLong(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestWriteTooLong", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		MaxSize:    5,
	}
	defer l.Close()
	b := []byte("booooooooooooooo!")
	n, err := l.Write(b)
	notNil(err, t)
	equals(0, n, t)
	equals(err.Error(),
		fmt.Sprintf("write length %d exceeds maximum file size %d", len(b), l.MaxSize), t)
	_, err = os.Stat(logFile(dir))
	assert(os.IsNotExist(err), t, "File exists, but should not have been created")
}

func TestMakeLogDir(t *testing.T) {
	currentTime = fakeTime
	dir := time.Now().Format("TestMakeLogDir" + format)
	dir = filepath.Join(os.TempDir(), dir)
	defer os.RemoveAll(dir)
	l := &Logger{
		Dir:        dir,
		NameFormat: format,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithLen(logFile(dir), n, t)
	fileCount(dir, 1, t)
}

func TestDefaultLogDir(t *testing.T) {
	currentTime = fakeTime
	dir := os.TempDir()
	l := &Logger{
		NameFormat: format,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	defer os.Remove(logFile(dir))

	isNil(err, t)
	equals(len(b), n, t)
	existsWithLen(logFile(dir), n, t)
}

func TestDefaultFilename(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestDefaultFilename", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Dir: dir,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	name := filepath.Join(dir, fakeTime().UTC().Format(defaultNameFormat))
	existsWithLen(name, n, t)
	fileCount(dir, 1, t)
}

func TestAutoRotate(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestAutoRotate", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		MaxSize:    10,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	filename := logFile(dir)
	existsWithLen(filename, n, t)
	fileCount(dir, 1, t)

	// set the current time one day later
	newFakeTime()

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	newFilename := logFile(dir)
	existsWithLen(newFilename, n, t)

	// make sure the old file still exists with the same size.
	existsWithLen(filename, len(b), t)

	fileCount(dir, 2, t)
}

func TestFirstWriteRotate(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestFirstWriteRotate", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		MaxSize:    10,
	}
	defer l.Close()

	filename := logFile(dir)
	err := ioutil.WriteFile(filename, []byte("boooooo!"), 0600)
	isNil(err, t)

	// set the current time one day later
	newFakeTime()

	// this would make us rotate
	b := []byte("fooo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	filename2 := logFile(dir)
	existsWithLen(filename2, n, t)

	fileCount(dir, 2, t)
}

func TestMaxBackups(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestMaxBackups", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		MaxSize:    10,
		MaxBackups: 1,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	firstFilename := logFile(dir)
	existsWithLen(firstFilename, n, t)
	fileCount(dir, 1, t)

	// set the current time one day later
	newFakeTime()

	// this will put us over the max
	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	secondFilename := logFile(dir)
	existsWithLen(secondFilename, n, t)

	// make sure the old file still exists with the same size.
	existsWithLen(firstFilename, len(b), t)

	fileCount(dir, 2, t)

	// set the current time one day later
	newFakeTime()

	// this will make us rotate again
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	thirdFilename := logFile(dir)
	existsWithLen(thirdFilename, n, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// should only have two files in the dir still
	fileCount(dir, 2, t)

	// second file name should still exist
	existsWithLen(secondFilename, n, t)

	// should have deleted the first filename
	notExist(firstFilename, t)

	// now test that we don't delete directories or non-logfile files

	// set the current time one day later
	newFakeTime()

	// create a file that is close to but different from the logfile name.
	/// It shouldn't get caught by our deletion filters.
	notlogfile := logFile(dir) + ".foo"
	err = ioutil.WriteFile(notlogfile, []byte("data"), 0644)
	isNil(err, t)

	// Make a directory that exactly matches our log file filters... it still
	// shouldn't get caught by the deletion filter since it's a directory.
	notlogfiledir := logFile(dir)
	err = os.Mkdir(notlogfiledir, 0700)
	isNil(err, t)

	newFakeTime()

	// this will make us rotate again
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	fourthFilename := logFile(dir)
	existsWithLen(fourthFilename, n, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// We should have four things in the directory now - the 2 log files, the
	// not log file, and the directory
	fileCount(dir, 4, t)

	// second file name should still exist
	existsWithLen(thirdFilename, n, t)

	// should have deleted the first filename
	notExist(firstFilename, t)

	// the not-a-logfile should still exist
	exists(notlogfile, t)

	// the directory
	exists(notlogfiledir, t)
}

func TestMaxAge(t *testing.T) {
	currentTime = fakeTime

	// change how maxage is interpreted from days to milliseconds
	day = time.Millisecond

	// This test uses ModTime on files, and so we need to make sure we're using
	// the most current time possible.
	fakeCurrentTime = time.Now()
	dir := makeTempDir("TestMaxAge", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		MaxSize:    10,
		MaxAge:     10,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	filename := logFile(dir)
	existsWithLen(filename, n, t)
	fileCount(dir, 1, t)

	// We need to wait for wall clock time since MaxAge uses file ModTime, which
	// can't be mocked.
	<-time.After(50 * time.Millisecond)
	fakeCurrentTime = time.Now()

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	// We should have just one log file
	fileCount(dir, 1, t)

	// this will use the new fake time
	newFilename := logFile(dir)
	existsWithLen(newFilename, n, t)

	// we should have deleted the old file due to being too old
	notExist(filename, t)
}

func TestLocalTime(t *testing.T) {
	currentTime = fakeTime

	dir := makeTempDir("TestLocalTime", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		LocalTime:  true,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	filename := logFileLocal(dir)
	existsWithLen(filename, n, t)
}

func TestDefaultDirAndName(t *testing.T) {
	currentTime = fakeTime

	l := &Logger{MaxSize: Megabyte}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	filename := filepath.Join(os.TempDir(), fakeTime().UTC().Format(defaultNameFormat))
	defer os.Remove(filename)

	isNil(err, t)
	equals(len(b), n, t)

	existsWithLen(filename, n, t)

	err = l.Close()
	isNil(err, t)

	newFakeTime()

	// even though the old file is under MaxSize, we should write a new file
	// to prevent two processes using lumberjack from writing to the same file.
	n, err = l.Write(b)

	f2 := filepath.Join(os.TempDir(), fakeTime().UTC().Format(defaultNameFormat))
	defer os.Remove(f2)

	isNil(err, t)
	equals(len(b), n, t)

	existsWithLen(f2, n, t)
}

func TestRotate(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestRotate", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Dir:        dir,
		NameFormat: format,
		MaxBackups: 1,
		MaxSize:    Megabyte,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	filename := logFile(dir)
	existsWithLen(filename, n, t)
	fileCount(dir, 1, t)

	// set the current time one day later
	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	filename2 := logFile(dir)
	existsWithLen(filename2, 0, t)
	existsWithLen(filename, n, t)
	fileCount(dir, 2, t)

	// set the current time one day later
	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	filename3 := logFile(dir)
	existsWithLen(filename3, 0, t)
	existsWithLen(filename2, 0, t)
	fileCount(dir, 2, t)

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	existsWithLen(filename3, n, t)
}

func TestJson(t *testing.T) {
	data := []byte(`
{
	"dir": "foo",
	"nameformat": "bar",
	"maxsize": 5,
	"maxage": 10,
	"maxbackups": 3,
	"localtime": true
}`[1:])

	l := Logger{}
	err := json.Unmarshal(data, &l)
	isNil(err, t)
	equals("foo", l.Dir, t)
	equals("bar", l.NameFormat, t)
	equals(int64(5), l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
}

func TestYaml(t *testing.T) {
	data := []byte(`
dir: foo
nameformat: bar
maxsize: 5
maxage: 10
maxbackups: 3
localtime: true`[1:])

	l := Logger{}
	err := yaml.Unmarshal(data, &l)
	isNil(err, t)
	equals("foo", l.Dir, t)
	equals("bar", l.NameFormat, t)
	equals(int64(5), l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
}

func TestToml(t *testing.T) {
	data := `
dir = "foo"
nameformat = "bar"
maxsize = 5
maxage = 10
maxbackups = 3
localtime = true`[1:]

	l := Logger{}
	md, err := toml.Decode(data, &l)
	isNil(err, t)
	equals("foo", l.Dir, t)
	equals("bar", l.NameFormat, t)
	equals(int64(5), l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
	equals(0, len(md.Undecoded()), t)
}

// makeTempDir creates a file with a semi-unique name in the OS temp directory.
// It should be based on the name of the test, to keep parallel tests from
// colliding, and must be cleaned up after the test is finished.
func makeTempDir(name string, t testing.TB) string {
	dir := time.Now().Format(name + format)
	dir = filepath.Join(os.TempDir(), dir)
	isNilUp(os.Mkdir(dir, 0777), t, 1)
	return dir
}

// existsWithLen checks that the given file exists and has the correct length.
func existsWithLen(path string, length int, t testing.TB) {
	info, err := os.Stat(path)
	isNilUp(err, t, 1)
	equalsUp(int64(length), info.Size(), t, 1)
}

// logFile returns the log file name in the given directory for the current fake
// time.
func logFile(dir string) string {
	return filepath.Join(dir, fakeTime().UTC().Format(format))
}

// logFileLocal returns the log file name in the given directory for the current
// fake time using the local timezone.
func logFileLocal(dir string) string {
	return filepath.Join(dir, fakeTime().Format(format))
}

// fileCount checks that the number of files in the directory is exp.
func fileCount(dir string, exp int, t testing.TB) {
	files, err := ioutil.ReadDir(dir)
	isNilUp(err, t, 1)
	// Make sure no other files were created.
	equalsUp(exp, len(files), t, 1)
}

// newFakeTime sets the fake "current time" to one day later.
func newFakeTime() {
	fakeCurrentTime = fakeCurrentTime.Add(time.Hour * 24)
}

func notExist(path string, t testing.TB) {
	_, err := os.Stat(path)
	assertUp(os.IsNotExist(err), t, 1, "expected to get os.IsNotExist, but instead got %v", err)
}

func exists(path string, t testing.TB) {
	_, err := os.Stat(path)
	assertUp(err == nil, t, 1, "expected file to exist, but got error from os.Stat: %v", err)
}
