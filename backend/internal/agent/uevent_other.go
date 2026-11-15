//go:build !linux

package agent

import (
	"context"
	"errors"
)

// KernelUevents ist nur unter Linux verfügbar.
type KernelUevents struct{}

func (KernelUevents) Events(context.Context) (<-chan struct{}, error) {
	return nil, errors.New("uevents werden nur unter Linux unterstützt")
}
