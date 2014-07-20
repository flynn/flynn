package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/flynn/go-discoverd"
)

type configFile struct {
	Template *template.Template
	Target   *os.File
}

type configFiles []configFile

func (f *configFiles) String() string {
	return "<template>:<dest>"
}

func (f *configFiles) Set(val string) error {
	var file configFile
	names := strings.SplitN(val, ":", 2)
	if len(names) != 2 {
		return errors.New("malformed file specification, expected TEMPLATE:DESTINATION")
	}

	var err error
	file.Template, err = template.ParseFiles(names[0])
	if err != nil {
		return err
	}
	file.Target, err = os.Create(names[1])
	if err != nil {
		return err
	}
	*f = append(*f, file)
	return nil
}

type configure struct {
	clientCmd
	files  configFiles
	update chan struct{}
}

func (cmd *configure) Name() string { return "configure" }

func (cmd *configure) DefineFlags(fs *flag.FlagSet) {
	fs.Var(&cmd.files, "f", "config file template/dest pair")
}

func (cmd *configure) Run(fs *flag.FlagSet) {
	cmd.InitClient(false)

	args := fs.Args()
	if len(args) == 0 {
		fmt.Println("no command to exec")
		os.Exit(1)
		return
	}

	cmd.update = make(chan struct{})
	data := newConfigData(cmd)

	var exitCh chan uint
	var proc *exec.Cmd

	update := func() {
		if proc != nil {
			proc.Process.Signal(syscall.SIGTERM)
			select {
			case <-exitCh:
				break
			case <-time.After(5 * time.Second):
				if err := proc.Process.Kill(); err != nil {
					panic("failed to kill")
				}
				<-exitCh
			}
			proc = nil
			exitCh = nil
		}

		for _, f := range cmd.files {
			if _, err := f.Target.Seek(0, 0); err != nil {
				log.Fatal(err)
			}
			if err := f.Target.Truncate(0); err != nil {
				log.Fatal(err)
			}
			if err := f.Template.Execute(f.Target, data); err != nil {
				log.Fatal(err)
			}
			if err := f.Target.Sync(); err != nil {
				log.Fatal(err)
			}
		}

		proc = exec.Command(args[0], args[1:]...)
		attachCmd(proc)
		if err := proc.Start(); err != nil {
			log.Fatal(err)
		}
		exitCh = exitStatusCh(proc)
	}

	update()

	for {
		select {
		case <-cmd.update:

		drain:
			for {
				select {
				case <-cmd.update:
				default:
					break drain
				}
			}
			update()
		case exitStatus := <-exitCh:
			os.Exit(int(exitStatus))
		}
	}
}

func environMap() map[string]string {
	env := os.Environ()
	res := make(map[string]string, len(env))
	for _, e := range env {
		kv := strings.SplitN(e, "=", 2)
		res[kv[0]] = kv[1]
	}
	return res
}

func newConfigData(cmd *configure) *configData {
	return &configData{
		Env:      environMap(),
		cmd:      cmd,
		services: make(map[string]*serviceData),
	}
}

type configData struct {
	Env map[string]string

	cmd      *configure
	services map[string]*serviceData
}

func (c *configData) ServiceSet(name string) (*serviceData, error) {
	if _, ok := c.services[name]; !ok {
		ss, err := c.cmd.client.NewServiceSet(name)
		if err != nil {
			return nil, err
		}
		sd := &serviceData{ss: ss, cmd: c.cmd}
		go sd.monitor()
		c.services[name] = sd
	}
	return c.services[name], nil
}

type serviceData struct {
	ss     discoverd.ServiceSet
	cmd    *configure
	leader *discoverd.Service
	all    bool
	mtx    sync.Mutex
}

func (s *serviceData) Leader() *discoverd.Service {
	leader := s.ss.Leader()
	if leader == nil {
		leader = s.waitForService()
	}
	s.mtx.Lock()
	s.leader = leader
	s.mtx.Unlock()
	return leader
}

func (s *serviceData) Services() []*discoverd.Service {
	s.mtx.Lock()
	s.all = true
	s.mtx.Unlock()
	services := s.ss.Services()
	if len(services) == 0 {
		services = []*discoverd.Service{s.waitForService()}
	}
	return services
}

func (s *serviceData) waitForService() *discoverd.Service {
	ch := s.ss.Watch(true)
	defer s.ss.Unwatch(ch)
	for {
		select {
		case update := <-ch:
			if !update.Online {
				continue
			}
			s := &discoverd.Service{
				Name:    update.Name,
				Created: update.Created,
				Addr:    update.Addr,
				Attrs:   update.Attrs,
			}
			s.Host, s.Port, _ = net.SplitHostPort(s.Addr)
			return s
		case <-s.cmd.update:
			// drain update channel to prevent deadlocks
		}
	}
}

func (s *serviceData) Addrs() []string {
	s.mtx.Lock()
	s.all = true
	s.mtx.Unlock()
	addrs := s.ss.Addrs()
	if len(addrs) == 0 {
		addrs = []string{s.waitForService().Addr}
	}
	return addrs
}

func (s *serviceData) monitor() {
	for _ = range s.ss.Watch(false) {
		s.mtx.Lock()
		all := s.all
		leader := s.leader
		s.mtx.Unlock()

		if !all {
			currLeader := s.ss.Leader()
			if currLeader == nil && leader == nil || currLeader.Addr == leader.Addr {
				continue
			}
			s.mtx.Lock()
			s.leader = currLeader
			s.mtx.Unlock()
		}
		s.cmd.update <- struct{}{}
	}
}
