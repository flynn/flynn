# cryptorand
--
    import "github.com/wadey/cryptorand"

Package cryptorand provides a math/rand.Source implementation of crypto/rand

## Example

```go
r := rand.New(cryptorand.Source)
fmt.Println(r.Float64() == r.Float64())

// Output:
// false
```

## Usage

```go
var Source rand.Source
```
Source is a math/rand.Source backed by crypto/rand. Calling Seed() will result
in a panic.

#### func  NewSource

```go
func NewSource(rand io.Reader) rand.Source
```
NewSource returns a new rand.Source backed by the given random source. Calling
Seed() will result in a panic.
