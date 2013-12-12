package main

import (
	"testing"
)

func TestBlarg(t *testing.T) {
	println("Hello, world")

	// t.Fail()
}

func TestPush(t *testing.T) {
	e := PushEvent{
		Ref: "refs/heads/master",
		Repository: Repository{
			Name:         "tang",
			Organization: "example",
			Url:          ".",
		},
		After: "HEAD",
	}
	err := eventPush(e)
	if err != nil {
		t.Error(err)
	}
}

func TestEvent(t *testing.T) {
	err := handleEvent("push", []byte(`{
		"ref": "refs/heads/master",
		"repository": {"name": "tang", "organization": "example", "url": "."},
		"after": "HEAD"}`))

	if err != nil {
		t.Error(err)
	}
}
