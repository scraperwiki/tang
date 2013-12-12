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
			Url: "file:///home/pwaller/sw/tang",
		},
		After: "HEAD",
	}
	eventPush(e)
}
