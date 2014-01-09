package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dustin/go-follow"
	"github.com/gorilla/websocket"
	"github.com/golang/groupcache/lru"
)

var (
	address        = flag.String("address", ":8080", "address to listen on")
	repositories   = flag.String("repositories", "scraperwiki/tang", "colon separated list of repositories to watch")
	allowedPushers = flag.String("allowed-pushers", "drj11:pwaller", "list of people allowed")
	uid            = flag.Int("uid", 0, "uid to run as")

	github_user, github_password string

	allowedPushersSet = map[string]bool{}

	// Populated by `go install -ldflags '-X tangRev asdf -X tangDate asdf'
	tangRev, tangDate string
)

func init() {
	flag.Parse()
	for _, who := range strings.Split(*allowedPushers, ":") {
		allowedPushersSet[who] = true
	}
	github_user = os.Getenv("GITHUB_USER")
	github_password = os.Getenv("GITHUB_PASSWORD")
	env := os.Environ()
	os.Clearenv()
	for _, e := range env {
		if strings.HasPrefix(e, "GITHUB_") {
			continue
		}
		split := strings.SplitN(e, "=", 2)
		key, value := split[0], split[1]
		os.Setenv(key, value)
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func ensureChildDeath() {
        sid, err := syscall.Setsid()
        if err != nil {
                sid = os.Getpid()
                }
        shc := fmt.Sprintf("trap 'kill -TERM -%d' HUP; while : ; do sleep 0.1; done", sid)
        c := exec.Command("bash", "-c", shc)
        c.Stdout = os.Stdout
        c.Stderr = os.Stderr
        c.SysProcAttr = &syscall.SysProcAttr{
                Pdeathsig: syscall.SIGHUP, 
        }
        err = c.Start()
        check(err)
        log.Println("Started..")

        go func() {
                err = c.Wait()
                log.Println("Exited sentinel..", err)
        }()
}


func main() {
        ensureChildDeath()
	if tangRev == "" {
		log.Println("tangRev and tangDate unavailable.")
		log.Println("Use install-tang script if you want build date/version")
	} else {
		log.Println("Starting", tangRev[:4], "committed", tangDate)
	}

	// Get the socket quickly so we can drop privileges ASAP
	listener, err := getListener(*address)
	check(err)

	// Must read exe before the executable is replaced by deployment
	// Must also read exe link before Setuid since we lose the privilege of
	// reading it.
	exe, err := os.Readlink("/proc/self/exe")
	check(err)

	// Drop privileges immediately after getting socket
	if *uid != 0 {
		panic("setuid is not supported, see http://code.google.com/p/go/issues/detail?id=1435")
		log.Println("Setting UID =", *uid)
		err = syscall.Setuid(*uid)
		check(err)
	}

	err = gitSetupCredentialHelper()
	check(err)

	// Start catching signals early.
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	// Make somewhere to put our logs
	err = os.MkdirAll("logs/", 0777)
	check(err)

	go ServeHTTP(listener)

	// Set up github hooks
	configureHooks()

	go func() {
		// Hack to let github know that the process started successfully
		// (Since the previous one may have been killed)
		infoURL := "http://services.scraperwiki.com/tang/"
		s := GithubStatus{"success", infoURL, "Tang running"}
		updateStatus("scraperwiki/tang", tangRev, s)
	}()

	// Tell the user how to quit
	if IsTerminal(os.Stdin.Fd()) {
		log.Println("Hello, terminal user. CTRL-D (EOF) to exit.")
		go ExitOnEOF()
	} else {
		log.Println("Send me SIGQUIT to exit.")
	}

	// Wait for a signal listed in `signal.Notify(sig, ...)`
	value := <-sig
	signal.Stop(sig)

	log.Printf("Received %v", value)

	if value == syscall.SIGTERM {
		return
	}

	// We've been instructed to exit.
	log.Printf("Revision %v exiting, restarting...", (tangRev + "doge")[:4])

	// TODO(pwaller) Don't exec before everything else has finished.
	// OTOH, that means waiting for other cruft in the pipeline, which
	// might cause a significant delay.
	// Maybe the process we exec to can wait on the children?
	// This is probably very tricky to get right without delaying the exec.
	// How do we find our children? Might involve iterating through /proc.

	err = syscall.Exec(exe, os.Args, gitCredentialsEnviron())
	check(err)
}

// Set up github hooks so that it notifies us for any chances to repositories
// we care about
func configureHooks() {

	if *repositories == "" {
		return
	}

	// JSON payload for github
	// http://developer.github.com/v3/repos/hooks/#json-http
	json := `{
	"name": "web",
	"config": {"url": "http://services.scraperwiki.com/hook",
		"content_type": "json"},
	"events": ["push", "issues", "issue_comment",
		"commit_comment", "create", "delete",
		"pull_request", "pull_request_review_comment",
		"gollum", "watch", "release", "fork", "member",
		"public", "team_add", "status"],
	"active": true
	}`

	// Each of the repositories listed on the command line
	repos := strings.Split(*repositories, ":")

	for _, repo := range repos {
		response, resp, err := Github(json, "repos", repo, "hooks")
		if err == ErrSkipGithubEndpoint {
			continue
		}
		check(err)

		switch resp.StatusCode {
		default:
			log.Print(response)

		case 422:
			log.Println("Already hooked for", repo)
		}
	}

}

// Since CTRL-C is used for a reload, it's nice to have a way to exit (CTRL-D).
func ExitOnEOF() {
	func() {
		buf := make([]byte, 64*1024)
		for {
			_, err := os.Stdin.Read(buf)
			if err == io.EOF {
				log.Println("EOF, bye!")
				os.Exit(0)
			}
		}
	}()
}

type WebsocketWriter struct {
	*websocket.Conn
}

func (ww *WebsocketWriter) Write(data []byte) (n int, err error) {
	err = ww.WriteMessage(websocket.BinaryMessage, data)
	if err == nil {
		n = len(data)
	}
	return
}

func LiveLogHandler(response http.ResponseWriter, req *http.Request) {
	ws, err := websocket.Upgrade(response, req, nil, 1024, 1024)
	defer ws.Close()

	if _, ok := err.(websocket.HandshakeError); ok {
		http.Error(response, "Not a websocket handshake", 400)
		return
	} else if err != nil {
		log.Println(err)
		return
	}

	stationaryFd, err := os.Open("/home/pwaller/test.log")
	check(err)
	defer stationaryFd.Close()
	fd := follow.New(stationaryFd)

	go func() {
		var err error
		// Wait until the other end closes the connection or sends
		// a message.
		t, m, err := ws.ReadMessage()
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("LiveLogHandler(): error reading msg: ", err)
		}
		_, _ = t, m
		// Close the follow descripter, causes Copy to terminate
		_ = fd.Close()
	}()

	w := &WebsocketWriter{ws}
	// Blocks until web connection is closed.
	_, err = io.Copy(w, fd)
	// log.Println("Err =", err, n)
}

func ServeHTTP(l net.Listener) {
	// Expose logs directory
	pwd, err := os.Getwd()
	check(err)
	logDir := path.Join(pwd, "logs")

	logHandler := http.FileServer(http.Dir(logDir))

	log.Println("Serving logs at", logDir)

	handler := NewTangHandler()

	handler.HandleFunc("/tang/", handleTang)
	handler.HandleFunc("/tang/live/logs/", LiveLogHandler)
	handler.Handle("/tang/logs/", http.StripPrefix("/tang/logs/", logHandler))
	handler.HandleFunc("/hook", handleHook)

	err = http.Serve(l, handler)
	log.Fatal(err)
}

type TangHandler struct {
	*http.ServeMux
	requests chan<- Request
}

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
	s.port = 8888
	s.cmd = exec.Command("sh", "-c", fmt.Sprintf(
		`while :; do printf "HTTP/1.1 200 OK\n\n$(date)" | nc -l %d; done`, s.port))
        s.cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig:syscall.SIGHUP}
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

func NewTangHandler() *TangHandler {
	requests := make(chan Request)
	go ServerRouter(requests)
	return &TangHandler{http.NewServeMux(), requests}
}

func (th *TangHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("Incoming request: %v %v", r.Host, r.URL)

	if th.HandleQA(w, r) {
		return
	}

	// Delegate
	th.ServeMux.ServeHTTP(w, r)
}

// TODO(drj): add params here on the left of the branch.
var checkQA, _ = regexp.Compile(`^([^.]+).([^.]+).qa.scraperwiki.com(:\d+)?`)

func (th *TangHandler) HandleQA(w http.ResponseWriter, r *http.Request) (handled bool) {
	pieces := checkQA.FindStringSubmatch(r.Host)
	if pieces == nil {
		return
	}
	handled = true

	ref, repository := pieces[1], pieces[2]
	_, _ = ref, repository

	//fmt.Fprintf(w, "TODO, proxy for %v %v %v", r.Host, ref, repository)
	serverChan := make(chan Server)
	th.requests <- Request{r.Host, serverChan}
	server := <-serverChan
	_, err := server.ready()
	if err != nil {
		http.Error(w, fmt.Sprintf("TANG Error from server: %q",
			err), 500)
		return
	}
	p := httputil.NewSingleHostReverseProxy(server.url())
	p.ServeHTTP(w, r)
	return
}

func handleTang(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = []string{"text/plain; charset=utf-8"}
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, `<!DOCTYPE html><style>html, body { font-type: sans; }</style><pre id="content"><pre>`)

	for i := 0; i < 100; i++ {
		fmt.Fprintf(w, "%d elephants\n", i)
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)
	}

	// fmt.Fprintf(w, `<script>window.location = "http://duckduckgo.com";</script>`)
}
