//go:build !(aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris)

package main

import "os"

func notifyResize(ch chan<- os.Signal) func() {
	return func() {}
}
