/**
 * Copyright 2018 gd Author. All Rights Reserved.
 * Author: Chuck1024
 */

package gd

import (
	"os"
	"os/signal"
	"syscall"
)

var (
	shutdown = make(chan os.Signal)
	running  = make(chan bool)
	hup      = make(chan os.Signal)
)

func init() {
	signal.Notify(shutdown, syscall.SIGINT, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGUSR2)
	signal.Notify(hup, syscall.SIGHUP)
}

func (e *Engine) Signal() {
	go func() {
		for {
			select {
			case sig := <-shutdown:
				Info("receive signal: %v, to stop server...", sig)
				running <- false
			case <-hup:
			}
		}
	}()
	Info("register signal ok")
}

func Close() {
	<- running
	close(running)
}
