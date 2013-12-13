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

func main() {
	// Start catching signals early.
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT)

	// Must read exe before the executable is replaced
	exe, err := os.Readlink("/proc/self/exe")
	check(err)

	l, err := getListener(*address)
	check(err)

	err = os.MkdirAll("logs/", 0777)
	check(err)

	go func() {
		http.HandleFunc("/hook", handleHook)

		http.Handle("/tang/logs/", http.FileServer(http.Dir("logs/")))
		// http.Handle(pattern, handler)

		// Drop privileges just before serving
		if *uid != 0 {
			log.Println("Setting UID =", *uid)
			err = syscall.Setreuid(*uid, *uid)
			check(err)
		}

		err = http.Serve(l, nil)
		log.Fatal(err)
	}()

	go func() {
		buf := make([]byte, 64*1024)
		for {
			_, err := os.Stdin.Read(buf)
			if err == io.EOF {
				log.Println("EOF, bye!")
				os.Exit(0)
			}
		}
	}()

	configureHooks()

	value := <-sig
	signal.Stop(sig)

	log.Printf("Recieved %v, restarting", value)

	// TODO(pwaller) Don't exec before everything else has finished.
	// OTOH, that means waiting for other cruft in the pipeline, which
	// might cause a significant delay.
	// Maybe the process we exec to can wait on the children?
	// This is probably very tricky to get right without delaying the exec.

	err = syscall.Exec(exe, os.Args, os.Environ())
	check(err)
}

func configureHooks() {

	if *repositories == "" {
		return
	}

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

func handleEvent(eventType string, document []byte) (err error) {

	// log.Println("Incoming request:", string(document))

	switch eventType {
	case "push":

		var event PushEvent
		err = json.Unmarshal(document, &event)
		if err != nil {
			return
		}

		log.Println("Received PushEvent %#+v", event)

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

func handleHook(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK\n")

	request, err := ioutil.ReadAll(r.Body)
	check(err)

	var buf bytes.Buffer
	// r.Header.Write(&buf)
	// log.Println("Incoming request headers: ", string(buf.Bytes()))
	// buf.Reset()

	err = json.Indent(&buf, request, "", "  ")
	check(err)

	eventType := r.Header["X-Github-Event"][0]
	data := buf.Bytes()

	err = handleEvent(eventType, data)
	check(err)
}

func Command(workdir, command string, args ...string) *exec.Cmd {
	log.Printf("wd = %s cmd = %s, args = %q", workdir, command, append([]string{}, args...))
	cmd := exec.Command(command, args...)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func runTang(repo, sha, repo_path, ref string) (err error) {

	c := `D=$TANG_LOGDIR/$TANG_REF/log; ./tang.hook |& tee $D/log.txt`
	cmd := Command(repo_path, "bash", "-c", c)

	pwd, err := os.Getwd()
	if err != nil {
		err = fmt.Errorf("runTang/getwd %q", err)
		return
	}

	logdir := path.Join(pwd, "logs/")
	cmd.Env = append(os.Environ(), "TANG_REF="+ref, "TANG_LOGDIR="+logdir)
	cmderr := cmd.Run()

	if cmderr == nil {
		// All OK, send along a green
		s := GithubStatus{"success", "http://services.scraperwiki.com", "All OK"}
		updateStatus(repo, sha, s)

		return
	}
	// else if err, ok := cmderr.(*exec.ExitError); ok {
	// status := err.Sys().(syscall.WaitStatus).ExitStatus()
	// _ = status
	// Not OK, send along red.
	s := GithubStatus{"failure", "http://services.scraperwiki.com", cmderr.Error()}
	updateStatus(repo, sha, s)

	return
}

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

	status := GithubStatus{"pending", "http://services.scraperwiki.com", "Running"}
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

	// Checkout the target sha (only use 6 characters of sha)
	checkout_dir := path.Join("checkout", event.After[:6])
	err = gitCheckout(git_dir, checkout_dir, event.After)
	if err != nil {
		return
	}

	log.Println("Created", checkout_dir)

	if event.NonGithub.NoBuild {
		// Bail out. This is here so that the tests can avoid running
		// themselves.
		return
	}

	// Run the tang script
	repo_workdir := path.Join(git_dir, checkout_dir)
	err = runTang(gh_repo, event.After, repo_workdir, event.Ref)
	if err != nil {
		return
	}

	return
}
