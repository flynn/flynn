package httpclient

import (
	"bufio"
	"io"
	"net/http"
	"reflect"

	"github.com/flynn/flynn/pkg/sse"
	"github.com/flynn/flynn/pkg/stream"
)

/*
	Stream manufactures a `pkg/stream.Stream`, starts a worker pumping events out of decoding, and returns that.

	The 'outputCh' parameter must be a sendable channel.  The "zero"-values of channel's content type will be created and used in the deserialization, then sent.

	The return values from `httpclient.RawReq` are probably a useful starting point for the 'res' parameter.

	Closing the returned `stream.Stream` shuts down the worker.
*/
func Stream(res *http.Response, outputCh interface{}) stream.Stream {
	stream := stream.New()
	chanValue := reflect.ValueOf(outputCh)
	stopChanValue := reflect.ValueOf(stream.StopCh)
	msgType := chanValue.Type().Elem().Elem()
	go func() {
		done := make(chan struct{})
		defer func() {
			chanValue.Close()
			close(done)
		}()

		go func() {
			select {
			case <-stream.StopCh:
			case <-done:
			}
			res.Body.Close()
		}()

		r := bufio.NewReader(res.Body)
		dec := sse.NewDecoder(r)
		for {
			msg := reflect.New(msgType)
			if err := dec.Decode(msg.Interface()); err != nil {
				if err != io.EOF {
					stream.Error = err
				}
				break
			}
			chosen, _, _ := reflect.Select([]reflect.SelectCase{
				{
					Dir:  reflect.SelectRecv,
					Chan: stopChanValue,
				},
				{
					Dir:  reflect.SelectSend,
					Chan: chanValue,
					Send: msg,
				},
			})
			switch chosen {
			case 0:
				return
			default:
			}
		}
	}()
	return stream
}
