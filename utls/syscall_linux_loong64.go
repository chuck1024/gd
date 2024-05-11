//go:build linux && loong64
// +build linux,loong64

/**
 * Copyright 2023 gd Author. All rights reserved.
 * Author: Chuck1024
 */

package utls

import (
	"syscall"
)

func Dup2(from, to int) error {
	if err := syscall.Dup3(from, to, 0); err != nil {
		return err
	}
	return nil
}
