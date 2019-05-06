package sparse

import (
	"sync"

	log "github.com/sirupsen/logrus"
)

var failFileHashMatch = false
var mutex sync.Mutex

// SetFailPointFileHashMatch simulates file hash match failure
func SetFailPointFileHashMatch(fail bool) {
	mutex.Lock()
	failFileHashMatch = fail
	mutex.Unlock()
}

// FailPointFileHashMatch returns true if this failpoint is set, clears the failpoint
func FailPointFileHashMatch() bool {
	mutex.Lock()
	val := failFileHashMatch
	if val {
		log.Warn("FailPointFileHashMatch!")
		failFileHashMatch = false
	}
	mutex.Unlock()
	return val
}
