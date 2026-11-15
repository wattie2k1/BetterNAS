//go:build !linux

package main

import "errors"

func reboot() error { return errors.New("neustart wird nur unter Linux unterstützt") }
