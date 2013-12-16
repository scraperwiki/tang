package main

// A listener which supports re-execing ourselves

import (
	"fmt"
	"log"
	"net"
	"os"
)

// Obtain listener by either taking it from `TANG_LISTEN_FD` if set, or
// net.Listen otherwise.
func getListener(address string) (l net.Listener, err error) {
	var fd uintptr
	if _, err = fmt.Sscan(os.Getenv("TANG_LISTEN_FD"), &fd); err == nil {
		var listener_fd *os.File
		listener_fd, err = InheritFd(fd)
		if err != nil {
			return
		}

		l, err = net.FileListener(listener_fd)
		if err != nil {
			err = fmt.Errorf("FileListener: %q", err)
			return
		}

		return
	}

	l, err = net.Listen("tcp4", address)
	if err != nil {
		err = fmt.Errorf("unable to listen: %q", err)
		return
	}
	log.Println("Listening on:", address)

	fd = GetFd(l)
	err = noCloseOnExec(fd)
	if err != nil {
		return
	}

	err = os.Setenv("TANG_LISTEN_FD", fmt.Sprintf("%d", fd))
	if err != nil {
		return
	}

	return
}
