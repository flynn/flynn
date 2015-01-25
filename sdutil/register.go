package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/flynn/flynn/discoverd/client"
)

type register struct {
	exitStatus   int
	exitSignalCh chan os.Signal
	host         *string
	netInterface *string
}

func (cmd *register) Name() string {
	return "register"
}

func (cmd *register) DefineFlags(fs *flag.FlagSet) {
	cmd.SetRegisterFlags(fs)
}

func (cmd *register) SetRegisterFlags(fs *flag.FlagSet) {
	cmd.host = fs.String("h", "", "Specify a particular host for the service")
	cmd.netInterface = fs.String("i", "", "Interface ip to use for this service")
}

func (cmd *register) ValidateFlags() {
	if *cmd.host != "" && *cmd.netInterface != "" {
		fmt.Println("specify either -h or -i")
		os.Exit(1)
	}

	if *cmd.netInterface != "" {
		iface, err := net.InterfaceByName(*cmd.netInterface)
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(65)
		}
		addr, err := iface.Addrs()
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(65)
		}
		if len(addr) == 0 {
			fmt.Printf("error: interface %s has no ip address\n", *cmd.netInterface)
			os.Exit(65)
		}
		// Split off network mask, wrap IPv6 hosts
		*cmd.host = strings.SplitN(addr[0].String(), "/", 2)[0]
		if strings.Contains(*cmd.host, ":") {
			*cmd.host = fmt.Sprintf("[%s]", *cmd.host)
		}
	}
}

func (cmd *register) RegisterWithExitHook(services map[string]string, verbose bool) {
	cmd.exitSignalCh = make(chan os.Signal, 1)
	signal.Notify(cmd.exitSignalCh, os.Interrupt, syscall.SIGTERM)
	hbs := make([]discoverd.Heartbeater, 0, len(services))
	for name, port := range services {
		hb, err := discoverd.DefaultClient.AddServiceAndRegister(name, *cmd.host+":"+port)
		if err != nil {
			log.Fatal(err)
		}
		hbs = append(hbs, hb)
	}
	go func() {
		<-cmd.exitSignalCh
		if verbose {
			log.Println("Unregistering service...")
		}
		for _, hb := range hbs {
			hb.Close()
		}
		os.Exit(cmd.exitStatus)
	}()
}

func (cmd *register) Run(fs *flag.FlagSet) {
	cmd.exitStatus = 0

	colonIdx := strings.LastIndex(fs.Arg(0), ":")
	if colonIdx == -1 {
		fmt.Println("Error: specify service in name:port format:", fs.Arg(0))
		os.Exit(1)
	}
	name := fs.Arg(0)[0:colonIdx]
	port := fs.Arg(0)[colonIdx+1:]

	cmd.RegisterWithExitHook(map[string]string{name: port}, true)

	log.Printf("Registered service '%s' on port %s.", name, port)
	for {
		time.Sleep(time.Second)
	}
}
