package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

var address = flag.String("address", ":8080", "address to listen on")
var repositories = flag.String("repositories", "scraperwiki/tang", "colon separated list of repositories to watch")

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	flag.Parse()

	configureHooks()

	http.HandleFunc("/hook", handleHook)

	log.Fatal(http.ListenAndServe(*address, nil))
}

func configureHooks() {
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

func handleHook(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!\n")

	request, err := ioutil.ReadAll(r.Body)
	check(err)

	var dst bytes.Buffer
	r.Header.Write(&dst)
	log.Println("Incoming request headers: ", string(dst.Bytes()))

	dst.Reset()
	err = json.Indent(&dst, request, "", "  ")
	check(err)

	var doc_ interface{}
	err = json.Unmarshal(dst.Bytes(), &doc_)
	check(err)
	doc := doc_.(map[string]interface{})

	log.Println("Incoming request:", string(dst.Bytes()))

	switch eventType := r.Header["X-Github-Event"][0]; eventType {
	case "push":

		log.Println("Pushed")
		ref := doc["ref"].(string)
		url := doc["repository"].(map[string]interface{})["url"].(string)
		after := doc["after"].(string)

		log.Println("Push to", url, ref, "after", after)

	default:
		log.Println("Unhandled event:", eventType)
	}

}
