package stream

import (
	"fmt"
	"log"
)

func ExampleHappyStream() {
	output := make(chan *exampleWork)
	stream, err := workerFunction(2, output)
	if err != nil {
		log.Fatalf("stream initialization failed: %s\n", err)
	}

	// Read all the data.
	for chunk := range output {
		fmt.Printf("chunk %s\n", chunk.msg)
	}
	fmt.Printf("end\n")
	fmt.Printf("stream.err: %v\n", stream.Err())

	// Output:
	// chunk 1
	// chunk 2
	// end
	// stream.err: <nil>
}

func ExampleClosePushbackStream() {
	output := make(chan *exampleWork)
	stream, err := workerFunction(2, output)
	if err != nil {
		log.Fatalf("stream initialization failed: %s\n", err)
	}

	// Start getting data...
	chunk := <-output
	fmt.Printf("chunk %s\n", chunk.msg)

	// Now, say we've gotten enough data.
	// Or, another error occurred in the system we're pushing data into, and we've lost interest.
	// (Maybe we're supplying data to an HTTP stream, and the client disconnected, for example.)
	stream.Close()

	fmt.Printf("end\n")
	fmt.Printf("stream.err: %v\n", stream.Err())

	// Output:
	// chunk 1
	// end
	// stream.err: <nil>
}

func ExampleErrorDuringStream() {
	output := make(chan *exampleWork)
	stream, err := workerFunctionBroken(1, output)
	if err != nil {
		log.Fatalf("stream initialization failed: %s\n", err)
	}

	// Read all the data.
	for chunk := range output {
		fmt.Printf("chunk %s\n", chunk.msg)
	}
	fmt.Printf("end\n")

	// There's been an error!
	fmt.Printf("stream.err: %v\n", stream.Err())
	// Notice: this was safe with the race detector on!
	// This is only race-safe because we we close the channel *after* setting s.err,
	// and the range on the output channel waits for that close!
	// If necessary, you could also construct `exampleWork` to have a mutex around err, but using the chan is easier.

	// Output:
	// chunk 1
	// end
	// stream.err: borkbork!
}

// Represents a piece of work that begins a request, and work continues pumping results in a goroutine.
// Imagine kicking off reading a bunch of data from the network, deserializing it, and passing on messages as a stream.
func workerFunction(volume int, output chan<- *exampleWork) (Stream, error) {
	stream := &exampleStream{
		stopCh: make(chan struct{}),
	}

	go func() {
		defer close(output)
		for i := 1; i <= volume; i++ {
			chunk := &exampleWork{msg: fmt.Sprintf("%d", i)}
			// This `select` is a necessary piece of hardening:
			// It makes it possible for the owner of the Stream reference we returned
			// to ask us nicely to stop pushing more data through.
			select {
			case output <- chunk:
			case <-stream.stopCh:
				return
			}
		}
	}()

	// You could also return nil an initialization error here, if there's an error before stream establishment.
	// For this example, nothing can go wrong, but we've included the signature for completeness.
	return stream, nil
}

// Same as `workerFunction`, but intentionally crippled to simulate an error.
func workerFunctionBroken(volume int, output chan<- *exampleWork) (Stream, error) {
	stream := &exampleStream{
		stopCh: make(chan struct{}),
	}

	go func() {
		defer close(output)
		for i := 1; i <= volume; i++ {
			output <- &exampleWork{msg: fmt.Sprintf("%d", i)}
		}
		// Suppose we got a piece that didn't deserialize, or the network pipe broke:
		stream.err = fmt.Errorf("borkbork!")
	}()

	return stream, nil
}

type exampleWork struct {
	msg string
}

type exampleStream struct {
	stopCh chan struct{}
	err    error
	// base chan<- *exampleWork // You *do not* need this!
}

func (s exampleStream) Close() error {
	close(s.stopCh)
	return nil
}

func (s exampleStream) Err() error {
	return s.err
}
