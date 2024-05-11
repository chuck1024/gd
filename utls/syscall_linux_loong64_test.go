//go:build linux && loong64
// +build linux,loong64

/**
 * Copyright 2023 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package utls

import (
	"log"
	"os"
	"testing"
)

func TestDup2(t *testing.T) {
	f := "./test.txt"
	file, err := os.Create(f)
	if err != nil {
		log.Fatal(err)
	}
	_, err = file.Write([]byte("hello world"))
	if err != nil {
		log.Fatal(err)
	}
	file.Close()
	open, err := os.Open(f)
	if err != nil {
		log.Fatal(err)
	}
	//defer open.Close()
	err = Dup2(int(open.Fd()), int(os.Stdout.Fd()))
	if err != nil {
		log.Fatal(err)
	}
}
