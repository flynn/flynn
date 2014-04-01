package comborpc

import (
	"io"
	"log"
	"mime"
	"net/http"

	"github.com/flynn/rpcplus"
	"github.com/flynn/rpcplus/jsonrpc"
)

type Server struct {
	s *rpcplus.Server
}

func New(s *rpcplus.Server) *Server {
	return &Server{s}
}

func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("405 must CONNECT\n"))
		return
	}

	var serve func(conn io.ReadWriteCloser)
	accept, _, _ := mime.ParseMediaType(req.Header.Get("Accept"))
	switch accept {
	case "application/vnd.flynn.rpc-hijack+json":
		serve = func(conn io.ReadWriteCloser) {
			codec := jsonrpc.NewServerCodec(conn)
			server.s.ServeCodec(codec)
		}
	default:
		serve = server.s.ServeConn
	}

	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking error:", req.RemoteAddr, ": ", err.Error())
		return
	}
	conn.Write([]byte("HTTP/1.0 200 Connected to Go RPC\n\n"))
	serve(conn)
}

func (server *Server) HandleHTTP(path string) {
	http.Handle(path, server)
}

func HandleHTTP() {
	New(rpcplus.DefaultServer).HandleHTTP(rpcplus.DefaultRPCPath)
}

func Register(rcvr interface{}) error {
	return rpcplus.Register(rcvr)
}

func RegisterName(name string, rcvr interface{}) error {
	return rpcplus.RegisterName(name, rcvr)
}
