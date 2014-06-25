// +build linux

package lumberjack_test

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/natefinch/lumberjack"
)

// Example of how to rotate in response to SIGHUP.
func ExampleLogger_Rotate() {
	l := &lumberjack.Logger{}
	log.SetOutput(l)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for {
			<-c
			l.Rotate()
		}
	}()
}

// To use lumberjack with the standard library's log package, just pass it into
// the SetOutput function when your application starts.
func Example() {
	log.SetOutput(&lumberjack.Logger{
		Dir:        "/var/log/myapp/",
		NameFormat: time.RFC822 + ".log",
		MaxSize:    lumberjack.Gigabyte,
		MaxBackups: 3,
		MaxAge:     28,
	})
}
