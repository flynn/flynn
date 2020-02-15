package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	numSlow = flag.Int("num-slow", 16, "Number of slow clients")
	host    = flag.String("host", "puma.1.localflynn.com", "Route host of puma app")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var wg sync.WaitGroup
	wg.Add(*numSlow)
	defer wg.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, os.Signal(syscall.SIGTERM))
		<-ch
		log.Print("stopping")
		cancel()
		wg.Wait()
	}()

	log.Printf("starting %d slow clients", *numSlow)
	for i := 0; i < *numSlow; i++ {
		go func(i int) {
			defer wg.Done()
			runSlow(ctx, i)
		}(i)
	}

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			log.Print("making normal request")
			res, err := http.Get("http://" + *host + "/fast")
			if err != nil {
				log.Print("ERROR:", err)
				continue
			}
			res.Body.Close()
			log.Printf("got response %s", res.Status)
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func runSlow(ctx context.Context, index int) {
	log.Printf("starting slow client %d", index)

	conn, err := net.Dial("tcp", *host+":80")
	if err != nil {
		log.Fatalf("error starting slow client %d: %s", index, err)
	}
	defer conn.Close()

	url := fmt.Sprintf("http://%s/slow/%d", *host, index)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("error starting slow client %d: %s", index, err)
	}
	if err := req.Write(conn); err != nil {
		log.Fatalf("error starting slow client %d: %s", index, err)
	}
	log.Printf("slow client %d has written request", index)

	<-ctx.Done()

	log.Printf("slow client %d reading response", index)
	res, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		log.Printf("slow client %d failed to read response: %s", index, err)
	}
	log.Printf("slow client %d got response %s", index, res.Status)
}
