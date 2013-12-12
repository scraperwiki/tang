package main

import (
	"os"
	"testing"
)

func init() {
	err := os.Setenv("TANG_TEST", "1")
	if err != nil {
		panic(err)
	}
}

func TestPush(t *testing.T) {

	e := PushEvent{
		Ref: "refs/heads/master",
		Repository: Repository{
			Name:         "tang",
			Organization: "example",
			Url:          ".",
		},
		After:     "HEAD",
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
	allowedPushersSet["testuser"] = true
	defer delete(allowedPushersSet, "testuser")
	err := handleEvent("push", []byte(`{
		"ref": "refs/heads/master",
		"repository": {"name": "tang", "organization": "example", "url": "."},
		"after": "HEAD",
		"pusher": {"name":"testuser"},
		"nongithub": {"nobuild": true}
		}`))

	if err != nil {
		t.Error(err)
	}
}
