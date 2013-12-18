package main

// Code for help interacting with the system

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"syscall"
	"unsafe"
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

// Keepalive is necessary otherwise go's use of SetFinalizer will .close() the
// file descriptors for us, which we don't want.
var keepalive = []*os.File{}

// Inherit a file descriptor from the process pre-fork.
// Linux only, uses /proc to find out what to call the descriptor.
// TODO(pwaller): This is probably un-necessary.
func InheritFd(fd uintptr) (file *os.File, err error) {
	fd_name, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return
	}
	file = os.NewFile(fd, fd_name)
	keepalive = append(keepalive, file)
	return
}

// IsTerminal returns true if the given file descriptor is a terminal.
// Shamelessly stolen from go.crypto
func IsTerminal(fd uintptr) bool {
	const ioctlReadTermios = syscall.TCGETS
	var termios syscall.Termios
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd,
		ioctlReadTermios, uintptr(unsafe.Pointer(&termios)), 0, 0, 0)
	return err == 0
}

// Invoke a `command` in `workdir` with `args`, connecting up its Stdout and Stderr
func Command(workdir, command string, args ...string) *exec.Cmd {
	log.Printf("wd = %s cmd = %s, args = %q", workdir, command, append([]string{}, args...))
	cmd := exec.Command(command, args...)
	cmd.Dir = workdir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}
