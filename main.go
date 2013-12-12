package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"
)

var address = flag.String("address", ":8080", "address to listen on")
var repositories = flag.String("repositories", "scraperwiki/tang", "colon separated list of repositories to watch")
var allowedPushers = flag.String("allowed-pushers", "drj11:pwaller", "list of people allowed")

var allowedPushersSet = map[string]bool{}

func init() {
	flag.Parse()
	for _, who := range strings.Split(*allowedPushers, ":") {
		allowedPushersSet[who] = true
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {

	// Must read exe before the executable is replaced
	exe, err := os.Readlink("/proc/self/exe")
	check(err)

	go func() {
		http.HandleFunc("/hook", handleHook)
		log.Println("Listening on:", *address)
		log.Fatal(http.ListenAndServe(*address, nil))
	}()

	configureHooks()

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT)
	<-sig
	signal.Stop(sig)

	log.Print("HUPPING!")

	log.Printf("My exe = %q", exe)

	err = syscall.Exec(exe, os.Args, os.Environ())
	check(err)
}

func configureHooks() {

	if *repositories == "" {
		return
	}

	github_user := os.Getenv("GITHUB_USER")
	github_password := os.Getenv("GITHUB_PASSWORD")

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

	endpoint := "https://" + github_user + ":" + github_password + "@" + "api.github.com"

	repos := strings.Split(*repositories, ":")

	for _, repo := range repos {
		log.Print("Repo: ", repo)

		buffer := strings.NewReader(json)
		resp, err := http.Post(endpoint+"/repos/"+repo+"/hooks", "application/json", buffer)
		check(err)

		log.Println("Rate Limit:", resp.Header["X-Ratelimit-Remaining"][0])

		switch resp.StatusCode {
		default:
			response, err := ioutil.ReadAll(resp.Body)
			check(err)

			log.Print(string(response))

		case 422:
			log.Println("Already hooked for", repo)
		}
	}

}

type Repository struct {
	Name         string `json:"name"`
	Url          string `json:"url"`
	Organization string `json:"organization"`
}

type Pusher struct {
	Name string `json:"name"`
}

type NonGithub struct {
	NoBuild bool `json:"nobuild"`
}

type PushEvent struct {
	Ref        string     `json:"ref"`
	Deleted    bool       `json:"deleted"`
	Repository Repository `json:"repository"`
	After      string     `json:"after"`
	Pusher     Pusher     `json:"pusher"`
	NonGithub  NonGithub  `json:"nongithub"`
}

func handleEvent(eventType string, document []byte) (err error) {

	log.Println("Incoming request:", string(document))

	switch eventType {
	case "push":
		var event PushEvent
		err = json.Unmarshal(document, &event)
		if err != nil {
			return
		}

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
	r.Header.Write(&buf)
	log.Println("Incoming request headers: ", string(buf.Bytes()))

	buf.Reset()
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

const GIT_BASE_DIR = "repo"

var (
	ErrEmptyRepoName         = errors.New("Empty repository name")
	ErrEmptyRepoOrganization = errors.New("Empty repository organization")
	ErrUserNotAllowed        = errors.New("User not in the allowed set")
)

// Creates or updates a mirror of `url` at `git_dir` using `git clone --mirror`
func gitLocalMirror(url, git_dir string) (err error) {

	err = os.MkdirAll(git_dir, 0777)
	if err != nil {
		return
	}

	err = Command(".", "git", "clone", "-q", "--mirror", url, git_dir).Run()

	if err == nil {
		log.Println("Cloned", url)

	} else if _, ok := err.(*exec.ExitError); ok {

		// Try "git remote update"
		err = Command(git_dir, "git", "fetch").Run()

		if err != nil {
			// git fetch where there is no update is exit status 1.
			if err.Error() != "exit status 1" {
				return
			}
		}

		log.Println("Remote updated", url)

	} else {
		return
	}

	return
}

func gitCheckout(git_dir, checkout_dir, ref string) (err error) {

	err = os.MkdirAll(path.Join(git_dir, checkout_dir), 0777)
	if err != nil {
		return
	}

	log.Println("Populating", checkout_dir)

	args := []string{"--work-tree", checkout_dir, "checkout", ref, "."}
	err = Command(git_dir, "git", args...).Run()
	if err != nil {
		return
	}

	return
}

func runTang(path, ref string) (err error) {
	cmd := Command(path, "./tang.hook")
	cmd.Env = append(os.Environ(), "TANG_REF="+ref)
	return cmd.Run()
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

	ref := event.Ref
	url := event.Repository.Url
	after := event.After

	log.Println("Push to", url, ref, "after", after)

	// The name of the subdirectory where the git
	// mirror is (or will appear, if it hasn't been
	// cloned yet).
	git_dir := path.Join(GIT_BASE_DIR, event.Repository.Organization,
		event.Repository.Name)
	err = gitLocalMirror(url, git_dir)
	if err != nil {
		return
	}

	checkout_dir := path.Join("checkout", after)
	err = gitCheckout(git_dir, checkout_dir, after)
	if err != nil {
		return
	}
	log.Println("Created", checkout_dir)

	if event.NonGithub.NoBuild {
		return
	}

	err = runTang(path.Join(git_dir, checkout_dir), event.Ref)
	if err != nil {
		return
	}

	return
}
