package main

// Code responsible for handling an incoming event from github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
)

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

	// Check to see if we have data from somewhere which is not github
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

	// Handle the event
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

// Invoked when a respository we are watching changes
func runTang(repo, sha, repo_path, ref, logPath string) (err error) {

	c := `./tang.hook |& tee $TANG_LOGPATH; exit ${PIPESTATUS[0]}`
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

	// Check if we there is a tang hook
	tang_hook_present, err := gitHaveFile(git_dir, event.After, "tang.hook")
	if err != nil || !tang_hook_present || event.NonGithub.NoBuild {
		// Bail out, error, no tang.hook or instructed not to build it.
		return
	}

	// Checkout the target sha
	err = gitCheckout(git_dir, checkout_dir, event.After)
	if err != nil {
		return
	}

	log.Println("Created", checkout_dir)

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
