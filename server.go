package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/url"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/golang/groupcache/lru"
)

type Request struct {
	server   string
	response chan<- Server
}

type Server interface {
	start(stuff string)
	stop()
	ready() (port uint16, err error)
	url() *url.URL
}

// Implementation of Server that uses exec() and normal Unix processes.
type execServer struct {
	cmd  *exec.Cmd
	port uint16
	up   chan struct{}
	err  error
}

func waitForListenerOn(port uint16) error {
	for spins := 0; spins < 600; spins++ {
		conn, err := net.Dial("tcp4", fmt.Sprintf(":%d", port))
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("Timed out trying to connect to :%d", port)
}

func (s *execServer) start(stuff string) {
	s.port = 32767 + rand.Int31n(32768)
	s.cmd = exec.Command("sh", "-c", fmt.Sprintf(
		`while :; do printf "HTTP/1.1 200 OK\n\n$(date)" | nc -l %d; done`, s.port))
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGHUP}
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr
	go func() {
		log.Printf("About to start %s %v", s.cmd.Path,
			s.cmd.Args)
		s.err = s.cmd.Start()
		if s.err == nil {
			s.err = waitForListenerOn(s.port)
		}
		log.Printf("Server ready. err: %q", s.err)
		close(s.up)
	}()
}

func (s *execServer) stop() {
	err := s.cmd.Process.Kill()
	if err != nil {
		log.Printf("Error when killing process on :%d : %q", s.port, err)
	}
}

func (s *execServer) ready() (port uint16, err error) {
	<-s.up
	return s.port, s.err
}

func (s *execServer) url() *url.URL {
	url, err := url.Parse(fmt.Sprintf("http://localhost:%d/", s.port))
	check(err)
	return url
}

func NewServer(request Request) Server {
	s := &execServer{}
	s.up = make(chan struct{})
	s.start("stuff probably dervied from request")
	return s
}

// Route qa servers (branch repo combination) to a port number.
// Starting a server if necessary.
func ServerRouter(requests <-chan Request) {
	cache := lru.New(5)
	cache.OnEvicted = func(key lru.Key, value interface{}) {
		value.(Server).stop()
	}

	for {
		request := <-requests
		value, ok := cache.Get(request.server)
		if !ok {
			value = NewServer(request)
			cache.Add(request.server, value)
		}
		request.response <- value.(Server)
	}
}
