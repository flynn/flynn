package main

import (
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"

	"github.com/flynn/flynn/discoverd/client"
)

/*
	ish: the Inexusable/Insecure/Internet SHell.
*/
func main() {
	name := os.Getenv("NAME")
	port := os.Getenv("PORT")
	addr := ":" + port
	if name == "" {
		name = "ish-service"
	}

	log.Println("Application", name, "(an ish instance) running")

	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	log.Println("Listening on", addr)

	if err := discoverd.Register(name, addr); err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/ish", ish)
	log.Fatal(http.Serve(l, nil))
}

func ish(resp http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.Error(resp, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	body, _ := ioutil.ReadAll(req.Body)

	cmd := exec.Command("/bin/sh", "-c", string(body)) // no bash in busybox
	cmd.Stdout = resp
	cmd.Stderr = resp
	cmd.Run()
}
