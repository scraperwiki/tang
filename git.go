package main

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
)

// everything git and github related

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
}

type PushEvent struct {
	Ref        string     `json:"ref"`
	Deleted    bool       `json:"deleted"`
	Repository Repository `json:"repository"`
	After      string     `json:"after"`
	Pusher     Pusher     `json:"pusher"`
	NonGithub  NonGithub  `json:"nongithub"`
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

func Github(payload string, endpoint ...string) (respString string, resp *http.Response, err error) {
	if os.Getenv("TANG_TEST") != "" {
		// don't touch the endpoint during tests.
		return
	}

	url := Endpoint(endpoint...)
	resp, err = http.Post(url, "application/json", strings.NewReader(payload))
	check(err)

	response, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	log.Println("Querying", url)
	log.Println("Rate Limit:", resp.Header["X-Ratelimit-Remaining"][0])

	return string(response), resp, err
}

// curl -d '{"state": "success", "target_url": "https://deleteme.pwaller.qa.scraperwiki.com/", "description": "Tests pass, deleteme.pwaller.qa.scraperwiki.com up"}' https://scraperwiki-salt:fe236ee1c96a3fcc5dd14047b3d35651ff9b919d@api.github.com/repos/scraperwiki/custard/statuses/c8e3e81f13ce73de42364fd61aae1216b5dc2b69
func updateStatus(repo, sha string, githubStatus GithubStatus) {
	bytes, err := json.Marshal(githubStatus)
	check(err)
	Github(string(bytes), "repos", repo, "statuses", sha)
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
