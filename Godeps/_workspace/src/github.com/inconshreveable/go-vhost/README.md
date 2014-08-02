# go-vhost
go-vhost is a simple library that lets you implement virtual hosting functionality for different protocols (HTTP and TLS so far). go-vhost has a high-level and a low-level interface. The high-level interface lets you wrap existing net.Listeners with "muxer" objects. You can then Listen() on a muxer for a particular virtual host name of interest which will return to you a net.Listener for just connections with the virtual hostname of interest.

The lower-level go-vhost interface are just functions which extract the name/routing information for the given protocol and return an object implementing net.Conn which works as if no bytes had been consumed.

### [API Documentation](https://godoc.org/github.com/inconshreveable/go-vhost)

### Usage

    import vhost "github.com/inconshreveable/go-vhost"

    // listen for connections on a port
    l, _ := net.Listen(":80")

    // start multiplexing on it
    mux, _ := vhost.NewHTTPMuxer(l, muxTimeout)

    // use the default error handler
    go mux.HandleErrors()

    // listen for connections to different domains
    for _, v := range virtualHosts {
	    vhost := v

	    // vhost.Name is a virtual hostname like "foo.example.com"
	    muxListener, _ := mux.Listen(vhost.Name)

	    go func() {
		    for {
			    conn, _ := muxListener.Accept()
			    go vhost.Handle(conn)
		    }
	    }()
    }

### Low-level API usage

    // accept a new connection
    conn, _ := listener.Accept()

    // parse out the HTTP request and the Host header
    if vhostConn, err = vhost.HTTP(conn); err != nil {
        panic("Not a valid http connection!")
    }

    fmt.Printf("Target Host: ", vhostConn.Host())
    // Target Host: example.com

    // vhostConn contains the entire request as if no bytes had been consumed
    bytes, _ := ioutil.ReadAll(vhostConn)
    fmt.Printf("%s", bytes)
    // GET / HTTP/1.1
    // Host: example.com
    // User-Agent: ...
    // ...


### Advanced introspection
The entire HTTP request headers are available for inspection in case you want to mux on something besides the Host header:

    // parse out the HTTP request and the Host header
    if vhostConn, err = vhost.HTTP(conn); err != nil {
        panic("Not a valid http connection!")
    }

    httpVersion := vhost.Request.MinorVersion
    customRouting := vhost.Request.Header["X-Custom-Routing-Header"]


Likewise for TLS, you can look at detailed information about the ClientHello message:

    if vhostConn, err = vhost.TLS(conn); err != nil {
        panic("Not a valid TLS connection!")
    }

    cipherSuites := vhost.ClientHelloMsg.CipherSuites
    sessionId := vhost.ClientHelloMsg.SessionId


##### Memory reduction with Free
After you're done muxing, you probably don't need to inspect the header data anymore, so you can make it available for garbage collection:

    // look up the upstream host
    upstreamHost := hostMapping[vhostConn.Host()]

    // free up the muxing data
    vhostConn.Free()

    // vhostConn.Host() == ""
    // vhostConn.Request == nil (HTTP)
    // vhostConn.ClientHelloMsg == nil (TLS)
