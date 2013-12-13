package main

import (
	"log"
	"os"
	"reflect"
	"runtime"
	"syscall"
)

// Obtain FD # without duping it. (naughty, but what are you going to do..)
func GetFd(l interface{}) uintptr {
	v := reflect.ValueOf(l).Elem().FieldByName("fd").Elem()
	return uintptr(v.FieldByName("sysfd").Int())
}

// These are here because there is no API in syscall for turning OFF
// close-on-exec (yet).

// from syscall/zsyscall_linux_386.go, but it seems like it might work
// for other platforms too.
func fcntl(fd int, cmd int, arg int) (val int, err error) {
	if runtime.GOOS != "linux" {
		log.Fatal("Function fcntl has not been tested on other platforms than linux.")
	}

	r0, _, e1 := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(cmd), uintptr(arg))
	val = int(r0)
	if e1 != 0 {
		err = e1
	}
	return
}

// Disable FD_CLOEXEC state for given `fd`
func noCloseOnExec(fd uintptr) (err error) {
	// getCloseOnExec(fd)
	_, err = fcntl(int(fd), syscall.F_SETFD, ^syscall.FD_CLOEXEC)
	// log.Println("Setting no close ", fd, err)
	// getCloseOnExec(fd)
	return
}

// Obtain FD_CLOEXEC state for given `fd`
func getCloseOnExec(fd uintptr) (result int, err error) {
	result, err = fcntl(int(fd), syscall.F_GETFD, ^syscall.FD_CLOEXEC)
	log.Println("Got close ", fd, result, err)
	return
}

// Debug function to show which fds are open (via ls -l /proc/self/fd)
func showfds() {
	self, err := os.Readlink("/proc/self")
	check(err)
	cmd := Command(".", "ls", "-l", "/proc/"+self+"/fd")
	err = cmd.Run()
	check(err)
}
