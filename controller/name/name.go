package name

import (
	"fmt"

	"github.com/dgryski/go-skip32"
)

var cipher, _ = skip32.New(make([]byte, 10))

func SetSeed(s []byte) {
	var err error
	cipher, err = skip32.New(s)
	if err != nil {
		panic(err)
	}
}

const max = uint32(len(animals) * len(verbs) * len(towns))

func Get(n uint32) string {
	for {
		// This implements the Cycle-Walking Cipher from "Ciphers with Arbitrary
		// Finite Domains" - http://www.cs.ucdavis.edu/~rogaway/papers/subset.pdf
		n = cipher.Obfus(n)
		if n < max {
			break
		}
	}
	a := int(n) % len(animals)
	v := int(n) / len(animals) % len(verbs)
	t := int(n) / len(animals) / len(verbs) % len(towns)
	return fmt.Sprintf("%s-%s-%s", animals[a], verbs[v], towns[t])
}
