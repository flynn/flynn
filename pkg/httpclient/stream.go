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

	The 'msgFactory' parameter is invoked to produce a structure that deserialized data is mapped into.

	The 'outputCh' parameter must be a sendable channel.  The channel's content type must be the same as the return type of the factory func.

	The return values from `httpclient.RawReq` are probably a useful starting point for the 'res' parameter.

	Closing the returned `stream.Stream` shuts down the worker.
*/
func Stream(res *http.Response, msgFactory func() interface{}, outputCh interface{}) stream.Stream {
	stream := stream.New()
	chanValue := reflect.ValueOf(outputCh)
	stopChanValue := reflect.ValueOf(stream.StopCh)
	go func() {
		defer func() {
			chanValue.Close()
			res.Body.Close()
		}()

		r := bufio.NewReader(res.Body)
		dec := sse.NewDecoder(r)
		for {
			msg := msgFactory()
			if err := dec.Decode(msg); err != nil {
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
					Send: reflect.ValueOf(msg),
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
