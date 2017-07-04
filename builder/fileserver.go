package main

import (
	"context"
	"io"
	"log"
	"net"

	p9p "github.com/flynn/go-p9p"
	"github.com/flynn/go-p9p/ufs"
)

func serveFilesystem(dir string, l net.Listener) {
	ctx := context.Background()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}

		go func(conn net.Conn) {
			defer conn.Close()

			ctx := context.WithValue(ctx, "conn", conn)
			session, err := ufs.NewSession(ctx, dir)
			if err != nil {
				log.Println("error creating session", err)
				return
			}

			if err := p9p.ServeConn(ctx, conn, p9p.Dispatch(session)); err != nil && err != io.EOF {
				log.Println("error serving conn:", err)
			}
		}(c)
	}
}
