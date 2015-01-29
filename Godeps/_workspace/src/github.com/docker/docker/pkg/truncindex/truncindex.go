package truncindex

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/flynn/flynn/Godeps/_workspace/src/github.com/tchap/go-patricia/patricia"
)

var (
	// ErrNoID is thrown when attempting to use empty prefixes
	ErrNoID        = errors.New("prefix can't be empty")
	errDuplicateID = errors.New("multiple IDs were found")
)

func init() {
	// Change patricia max prefix per node length,
	// because our len(ID) always 64
	patricia.MaxPrefixPerNode = 64
}

// TruncIndex allows the retrieval of string identifiers by any of their unique prefixes.
// This is used to retrieve image and container IDs by more convenient shorthand prefixes.
type TruncIndex struct {
	sync.RWMutex
	trie *patricia.Trie
	ids  map[string]struct{}
}

// NewTruncIndex creates a new TruncIndex and initializes with a list of IDs
func NewTruncIndex(ids []string) (idx *TruncIndex) {
	idx = &TruncIndex{
		ids:  make(map[string]struct{}),
		trie: patricia.NewTrie(),
	}
	for _, id := range ids {
		idx.addID(id)
	}
	return
}

func (idx *TruncIndex) addID(id string) error {
	if strings.Contains(id, " ") {
		return fmt.Errorf("illegal character: ' '")
	}
	if id == "" {
		return ErrNoID
	}
	if _, exists := idx.ids[id]; exists {
		return fmt.Errorf("id already exists: '%s'", id)
	}
	idx.ids[id] = struct{}{}
	if inserted := idx.trie.Insert(patricia.Prefix(id), struct{}{}); !inserted {
		return fmt.Errorf("failed to insert id: %s", id)
	}
	return nil
}

// Add adds a new ID to the TruncIndex
func (idx *TruncIndex) Add(id string) error {
	idx.Lock()
	defer idx.Unlock()
	if err := idx.addID(id); err != nil {
		return err
	}
	return nil
}

// Delete removes an ID from the TruncIndex. If there are multiple IDs
// with the given prefix, an error is thrown.
func (idx *TruncIndex) Delete(id string) error {
	idx.Lock()
	defer idx.Unlock()
	if _, exists := idx.ids[id]; !exists || id == "" {
		return fmt.Errorf("no such id: '%s'", id)
	}
	delete(idx.ids, id)
	if deleted := idx.trie.Delete(patricia.Prefix(id)); !deleted {
		return fmt.Errorf("no such id: '%s'", id)
	}
	return nil
}

// Get retrieves an ID from the TruncIndex. If there are multiple IDs
// with the given prefix, an error is thrown.
func (idx *TruncIndex) Get(s string) (string, error) {
	idx.RLock()
	defer idx.RUnlock()
	var (
		id string
	)
	if s == "" {
		return "", ErrNoID
	}
	subTreeVisitFunc := func(prefix patricia.Prefix, item patricia.Item) error {
		if id != "" {
			// we haven't found the ID if there are two or more IDs
			id = ""
			return errDuplicateID
		}
		id = string(prefix)
		return nil
	}

	if err := idx.trie.VisitSubtree(patricia.Prefix(s), subTreeVisitFunc); err != nil {
		return "", fmt.Errorf("no such id: %s", s)
	}
	if id != "" {
		return id, nil
	}
	return "", fmt.Errorf("no such id: %s", s)
}
