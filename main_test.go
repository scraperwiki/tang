package main

import (
	"log"
	"os"
	"testing"

	"github.com/kr/text"
)

func IndentLogger() func() {
	log.SetOutput(text.NewIndentWriter(os.Stderr, []byte("    ")))
	return func() {
		log.SetOutput(os.Stderr)
	}
}

func init() {
	err := os.Setenv("TANG_TEST", "1")
	if err != nil {
		panic(err)
	}
}

func TestTrivialRepo(t *testing.T) {
	cmd := Command("fixture", "tar", "xf", "trivial-repo.tar.bz2")
	err := cmd.Run()
	if err != nil {
		t.Error(err)
	}

	e := PushEvent{
		Ref: "refs/heads/master",
		Repository: Repository{
			Name:         "trivial-repo",
			Organization: "example",
			Url:          "fixture/trivial-repo",
		},
		After:  "master",
		Pusher: Pusher{Name: "testuser"},
	}

	allowedPushersSet["testuser"] = true
	defer delete(allowedPushersSet, "testuser")

	err = eventPush(e)
	if err != nil {
		t.Error(err)
	}
}

func TestPush(t *testing.T) {
	defer IndentLogger()()

	e := PushEvent{
		Ref: "refs/heads/master",
		Repository: Repository{
			Name:         "tang",
			Organization: "example",
			Url:          ".",
		},
		After:     "ee7c7b8f65dea5d3ef81c17eacd1b873be167109",
		Pusher:    Pusher{Name: "testuser"},
		NonGithub: NonGithub{NoBuild: true},
	}

	allowedPushersSet["testuser"] = true
	defer delete(allowedPushersSet, "testuser")

	err := eventPush(e)
	if err != nil {
		t.Error(err)
	}
}

func TestEvent(t *testing.T) {
	defer IndentLogger()()

	allowedPushersSet["testuser"] = true
	defer delete(allowedPushersSet, "testuser")

	err := handleEvent("push", []byte(`{
		"ref": "refs/heads/master",
		"repository": {"name": "tang", "organization": "example", "url": "."},
		"after": "ee7c7b8f65dea5d3ef81c17eacd1b873be167109",
		"pusher": {"name":"testuser"},
		"nongithub": {"nobuild": true}
		}`))

	if err != nil {
		t.Error(err)
	}
}

func TestAccess(t *testing.T) {
	defer IndentLogger()()

	allowedPushersSet["testuser"] = true
	defer delete(allowedPushersSet, "testuser")

	err := handleEvent("push", []byte(`{
		"ref": "refs/heads/master",
		"repository": {"name": "tang", "organization": "example", "url": "."},
		"after": "ee7c7b8f65dea5d3ef81c17eacd1b873be167109",
		"pusher": {"name":"testeviluser"},
		"nongithub": {"nobuild": true}
		}`))

	if err != ErrUserNotAllowed {
		t.Error("User wasn't denied access! ", err)
	}

}
