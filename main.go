package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"
)

var (
	address        = flag.String("address", ":8080", "address to listen on")
	repositories   = flag.String("repositories", "scraperwiki/tang", "colon separated list of repositories to watch")
	allowedPushers = flag.String("allowed-pushers", "drj11:pwaller", "list of people allowed")
	uid            = flag.Int("uid", 0, "uid to run as")

	github_user, github_password string

	allowedPushersSet = map[string]bool{}
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

// Obtain listener by either taking it from `TANG_LISTEN_FD` if set, or
// net.Listen otherwise.
func getListener(address string) (l net.Listener, err error) {
	var fd uintptr
	if _, err = fmt.Sscan(os.Getenv("TANG_LISTEN_FD"), &fd); err == nil {
		var listener_fd *os.File
		listener_fd, err = InheritFd(fd)
		if err != nil {
			return
		}

		l, err = net.FileListener(listener_fd)
		if err != nil {
			err = fmt.Errorf("FileListener: %q", err)
			return
		}

		return
	}

	l, err = net.Listen("tcp4", address)
	if err != nil {
		err = fmt.Errorf("unable to listen: %q", err)
		return
	}
	log.Println("Listening on:", address)

	fd = GetFd(l)
	err = noCloseOnExec(fd)
	if err != nil {
		return
	}

	err = os.Setenv("TANG_LISTEN_FD", fmt.Sprintf("%d", fd))
	if err != nil {
		return
	}

	return
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
	handler := http.FileServer(http.Dir(logDir))
	log.Println("Serving logs at", logDir)
	http.Handle("/tang/logs", http.StripPrefix("/tang/logs", handler))

	// Github hook handler
	http.HandleFunc("/hook", handleHook)

	err = http.Serve(l, nil)
	log.Fatal(err)
}

func main() {
	log.Println("Starting")
	// Get the socket quickly so we can drop privileges ASAP
	l, err := getListener(*address)
	check(err)

	// Drop privileges immediately after getting socket
	if *uid != 0 {
		log.Println("Setting UID =", *uid)
		err = syscall.Setreuid(*uid, *uid)
		check(err)
	}

	// Start catching signals early.
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT)

	// Must read exe before the executable is replaced
	exe, err := os.Readlink("/proc/self/exe")
	check(err)

	// Make somewhere to put our logs
	err = os.MkdirAll("logs/", 0777)
	check(err)

	go ServeHTTP(l)

	// Set up github hooks
	configureHooks()

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
	log.Printf("Recieved %v, restarting...", value)

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
		check(err)

		switch resp.StatusCode {
		default:
			log.Print(response)

		case 422:
			log.Println("Already hooked for", repo)
		}
	}

}

// This function is called whenever an event happens on github.
// Valid event types are
func handleEvent(eventType string, document []byte) (err error) {

	// log.Println("Incoming request:", string(document))

	switch eventType {
	case "push":

		var event PushEvent
		err = json.Unmarshal(document, &event)
		if err != nil {
			return
		}

		log.Printf("Received PushEvent %#+v", event)

		if event.Deleted {
			// When a branch is deleted we get a "push" event we don't care
			// about (after = "0000")
			return
		}

		err = eventPush(event)
		if err != nil {
			return
		}

	default:
		log.Println("Unhandled event:", eventType)
	}

	return
}

// HTTP handler for /hook
// It is expecting a POST with a JSON payload according to
// http://developer.github.com/v3/activity/events/
func handleHook(w http.ResponseWriter, r *http.Request) {

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, "Expected JSON POST payload.\n")
		return
	}

	request, err := ioutil.ReadAll(r.Body)
	check(err)

	var buf bytes.Buffer
	// r.Header.Write(&buf)
	// log.Println("Incoming request headers: ", string(buf.Bytes()))
	// buf.Reset()

	err = json.Indent(&buf, request, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Expected valid JSON POST payload.\n")
		log.Println("Not a valid JSON payload. NOOP.")
		return
	}

	if len(r.Header["X-Github-Event"]) != 1 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Expected X-Github-Event header.\n")
		log.Println("No X-Github-Event header. NOOP")
		return
	}
	eventType := r.Header["X-Github-Event"][0]
	data := buf.Bytes()

	j, err := ParseJustNongithub(request)
	if !j.NonGithub.Wait {
		go func() {
			err := handleEvent(eventType, data)
			if err != nil {
				log.Printf("Error processing %v %v %q", eventType, string(data), err)
			}
		}()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK. Not waiting for build.\n")
		return
	}

	err = handleEvent(eventType, data)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Error handling event: %q\n", err)
		log.Printf("Error handling event: %q", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK\n")
}

func Command(workdir, command string, args ...string) *exec.Cmd {
	log.Printf("wd = %s cmd = %s, args = %q", workdir, command, append([]string{}, args...))
	cmd := exec.Command(command, args...)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// Invoked when a respository we are watching changes
func runTang(repo, sha, repo_path, ref, logPath string) (err error) {

	// TODO(pwaller): determine lack of tang.hook?

	c := `echo Will try to write to $TANG_LOGPATH; ./tang.hook |& tee $TANG_LOGPATH; exit ${PIPESTATUS[0]}`
	cmd := Command(repo_path, "bash", "-c", c)

	cmd.Env = append(os.Environ(),
		"TANG_SHA="+sha, "TANG_REF="+ref, "TANG_LOGPATH="+logPath)
	err = cmd.Run()

	return
}

// Invoked when there is a push event to github.
func eventPush(event PushEvent) (err error) {
	if event.Repository.Name == "" {
		return ErrEmptyRepoName
	}

	if event.Repository.Organization == "" {
		return ErrEmptyRepoOrganization
	}

	if _, ok := allowedPushersSet[event.Pusher.Name]; !ok {
		log.Printf("Ignoring %q, not allowed", event.Pusher.Name)
		return ErrUserNotAllowed
	}

	gh_repo := path.Join(event.Repository.Organization, event.Repository.Name)

	// Only use 6 characters of sha for the name of the
	// directory checked out for this repository by tang.
	short_sha := event.After[:6]
	checkout_dir := path.Join("checkout", short_sha)

	pwd, err := os.Getwd()
	if err != nil {
		err = fmt.Errorf("runTang/getwd %q", err)
		return
	}

	logDir := path.Join("logs", short_sha)
	err = os.MkdirAll(logDir, 0777)
	if err != nil {
		err = fmt.Errorf("runTang/MkdirAll(%q): ", logDir, err)
		return
	}

	logPath := path.Join(logDir, "log.txt")
	fullLogPath := path.Join(pwd, logPath)

	// TODO(pwaller): One day this will have more information, e.g, QA link.
	infoURL := "http://services.scraperwiki.com/tang/" + logPath

	// Set the state of the commit to "in progress" (seen as yellow in
	// a github pull request)
	status := GithubStatus{"pending", infoURL, "Running"}
	updateStatus(gh_repo, event.After, status)

	log.Println("Push to", event.Repository.Url, event.Ref, "after", event.After)

	// The name of the subdirectory where the git
	// mirror is (or will appear, if it hasn't been
	// cloned yet).
	git_dir := path.Join(GIT_BASE_DIR, gh_repo)

	// Update our local mirror
	err = gitLocalMirror(event.Repository.Url, git_dir)
	if err != nil {
		return
	}

	// Checkout the target sha
	err = gitCheckout(git_dir, checkout_dir, event.After)
	if err != nil {
		return
	}

	log.Println("Created", checkout_dir)

	if event.NonGithub.NoBuild {
		// Bail out. This is here so that the tests
		// can avoid running themselves.
		return
	}

	// Run the tang script for the repository, if there is one.
	repo_workdir := path.Join(git_dir, checkout_dir)
	err = runTang(gh_repo, event.After, repo_workdir, event.Ref, fullLogPath)

	if err == nil {
		// All OK, send along a green
		s := GithubStatus{"success", infoURL, "Tests passed"}
		updateStatus(gh_repo, event.After, s)
		return
	}

	// Not OK, send along red.
	s := GithubStatus{"failure", infoURL, err.Error()}
	updateStatus(gh_repo, event.After, s)
	return
}
