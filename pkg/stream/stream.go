package stream

/*
	A Stream allows control over a stream sent to a channel.

	Typical usage is in a function signature resembling the following:
		func Follow(ch chan<- *Interesting) Stream

	Such a function signature is common when spawning a goroutine to produce
	information (one common example being a goroutine which receives information from
	the network, deserializes it, and then passes it on to another process as a
	stream, via the provided channel).  Returning the Stream interface solves the
	twin problems of allowing the worker goroutine to tell the consumer about later
	errors, and allowing the consumer to tell the worker that it's no longer interested
	in more information.

	Note that though this interface describes its operation in relationship
	to a channel, the channel is not included in the interface.  This is
	making an end-run around the lack of generics in golang.  Using the idiom
	above to provide the (typed!) channel as a function parameter and returning
	a Stream is the recommended mechanism for avoiding complication.
*/
type Stream interface {
	// Close signals the sender to stop sending and then closes the channel.
	Close() error

	// Err reads the error (if any) that occurred while receiving the stream. It
	// must only be called after the channel has been closed.
	Err() error
}
