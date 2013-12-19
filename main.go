package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
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
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	if tangRev == "" {
		log.Println("tangRev and tangDate unavailable.")
		log.Println("Use install-tang script if you want build date/version")
	} else {
		log.Println("Starting", tangRev[:4], "committed", tangDate)
	}

	// Get the socket quickly so we can drop privileges ASAP
	l, err := getListener(*address)
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

	// Start catching signals early.
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT)

	// Make somewhere to put our logs
	err = os.MkdirAll("logs/", 0777)
	check(err)

	go ServeHTTP(l)

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

	// We've been instructed to exit.
	log.Printf("Recieved %v, revision %v restarting...", value, tangRev[:4])

	// TODO(pwaller) Don't exec before everything else has finished.
	// OTOH, that means waiting for other cruft in the pipeline, which
	// might cause a significant delay.
	// Maybe the process we exec to can wait on the children?
	// This is probably very tricky to get right without delaying the exec.
	// How do we find our children? Might involve iterating through /proc.

	err = syscall.Exec(exe, os.Args, os.Environ())
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

func ServeHTTP(l net.Listener) {
	// Expose logs directory
	pwd, err := os.Getwd()
	check(err)
	logDir := path.Join(pwd, "logs")

	staticHandler := http.FileServer(http.Dir(logDir))

	log.Println("Serving logs at", logDir)

	handler := NewTangHandler()

	handler.HandleFunc("/tang/", handleTang)
	handler.Handle("/tang/logs/", http.StripPrefix("/tang/logs/", staticHandler))
	handler.HandleFunc("/hook", handleHook)

	err = http.Serve(l, handler)
	log.Fatal(err)
}

type TangHandler struct {
	*http.ServeMux
}

func NewTangHandler() *TangHandler {
	return &TangHandler{http.NewServeMux()}
}

func (th *TangHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("Incoming request: %v %v", r.Host, r.URL)
	if strings.HasSuffix(r.Host, ".qa.scraperwiki.com") {
		th.HandleQA(w, r)
		return
	}

	// Delegate
	th.ServeMux.ServeHTTP(w, r)
}

func (th *TangHandler) HandleQA(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "TODO, proxy for %v", r.Host)
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
