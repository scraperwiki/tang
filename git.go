package main

// everything git and github related

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const GIT_BASE_DIR = "repo"

var (
	ErrEmptyRepoName         = errors.New("Empty repository name")
	ErrEmptyRepoOrganization = errors.New("Empty repository organization")
	ErrUserNotAllowed        = errors.New("User not in the allowed set")
)

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
	Wait    bool `json:"wait"`
}

type JustNongithub struct {
	NonGithub NonGithub `json:"nongithub"`
}

func ParseJustNongithub(in []byte) (j JustNongithub, err error) {
	err = json.Unmarshal(in, &j)
	return
}

type PushEvent struct {
	Ref        string     `json:"ref"`
	Deleted    bool       `json:"deleted"`
	Repository Repository `json:"repository"`
	After      string     `json:"after"`
	Pusher     Pusher     `json:"pusher"`
	NonGithub  NonGithub  `json:"nongithub"`
	HtmlUrl    string     `json:"html_url"`
}

type GithubStatus struct {
	State       string `json:"state"`
	TargetUrl   string `json:"target_url"`
	Description string `json:"description"`
}

func Endpoint(args ...string) string {
	base := "https://" + github_user + ":" + github_password + "@" + "api.github.com/"
	return base + path.Join(args...)
}

var ErrSkipGithubEndpoint = errors.New("Github endpoint skipped")

func Github(payload string, endpoint ...string) (respString string, resp *http.Response, err error) {
	if os.Getenv("TANG_TEST") != "" {
		// don't touch the endpoint during tests.
		err = ErrSkipGithubEndpoint
		return
	}
	if github_user == "" {
		log.Printf("github_user not specified, not querying endpoint %q", endpoint)
		err = ErrSkipGithubEndpoint
		return
	}

	url := Endpoint(endpoint...)
	resp, err = http.Post(url, "application/json", strings.NewReader(payload))
	switch err {
	case io.EOF:
		// We've observed io.EOF here, and it is safe to ignore.
		log.Println("EOF: from", url, resp.Status, resp.Header)
	default:
		check(err)
	}

	response, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		if err == io.EOF {
			log.Println("EOF from", url, response)
		}
		return
	}

	re := regexp.MustCompile("://.*:.*@")
	urlNotSecret := re.ReplaceAllLiteralString(url, "://***@")
	log.Println("Querying", urlNotSecret)
	log.Println("Rate Limit:", resp.Header["X-Ratelimit-Remaining"][0])

	return string(response), resp, err
}

// curl -d '{"state": "success", "target_url": "https://deleteme.pwaller.qa.scraperwiki.com/", "description": "Tests pass, deleteme.pwaller.qa.scraperwiki.com up"}' https://scraperwiki-salt:fe236ee1c96a3fcc5dd14047b3d35651ff9b919d@api.github.com/repos/scraperwiki/custard/statuses/c8e3e81f13ce73de42364fd61aae1216b5dc2b69
func updateStatus(repo, sha string, githubStatus GithubStatus) {
	bytes, err := json.Marshal(githubStatus)
	check(err)
	Github(string(bytes), "repos", repo, "statuses", sha)
}

func gitSetupCredentialHelper() (err error) {
	cmd := Command(".", "git", "config", "--get", "credential.helper")
	err = cmd.Run()
	if err == nil {
		return
	}

	if err, ok := err.(*exec.ExitError); ok {
		if err.ProcessState.Sys().(syscall.WaitStatus).ExitStatus() == 1 {
			value := `!f() { echo username=$GITHUB_USER; echo password=$GITHUB_PASSWORD; }; f`
			cmd = Command(".", "git", "config", "--global", "credential.helper", value)
			return cmd.Run()
		}
	}
	return
}

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

		done := make(chan struct{})
		// Try "git remote update"

		cmd := Command(git_dir, "git", "fetch")
		cmd.Env = append(os.Environ(), "GIT_TRACE=1")
		go func() {
			err = cmd.Run()
			log.Printf("Normal completion of cmd %+v", cmd)
			close(done)
		}()

		const timeout = 20 * time.Second
		select {
		case <-done:
		case <-time.After(timeout):
			err = cmd.Process.Kill()
			log.Printf("Killing cmd %+v after %v, error returned: %v", cmd, timeout, err)
			err = fmt.Errorf("cmd %+v timed out", cmd)
		}

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

func gitHaveFile(git_dir, ref, path string) (ok bool, err error) {
	cmd := Command(git_dir, "git", "show", fmt.Sprintf("%s:%s", ref, path))
	cmd.Stdout = nil // don't want to see the contents
	err = cmd.Run()
	ok = true
	if err != nil {
		ok = false
		if err.Error() == "exit status 128" {
			// This happens if the file doesn't exist.
			err = nil
		}
	}
	return ok, err
}

func gitRevParse(git_dir, ref string) (sha string, err error) {
	cmd := Command(git_dir, "git", "rev-parse", ref)
	cmd.Stdout = nil // for cmd.Output

	var stdout []byte
	stdout, err = cmd.Output()
	if err != nil {
		return
	}

	sha = string(stdout)
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
