//go:build linux

package agent

import (
	"context"
	"log/slog"

	"golang.org/x/sys/unix"
)

// KernelUevents liest Kernel-Uevents über einen Netlink-Socket (NETLINK_KOBJECT_UEVENT, Gruppe 1).
type KernelUevents struct{}

func (KernelUevents) Events(ctx context.Context) (<-chan struct{}, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1}); err != nil {
		unix.Close(fd)
		return nil, err
	}
	ch := make(chan struct{}, 16)
	go func() {
		<-ctx.Done()
		unix.Close(fd) // beendet das blockierende Recvfrom
	}()
	go func() {
		defer close(ch)
		buf := make([]byte, 64*1024)
		for {
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				if ctx.Err() == nil {
					slog.Warn("uevent: lesen fehlgeschlagen", "err", err)
				}
				return
			}
			if isBlockUevent(buf[:n]) {
				select {
				case ch <- struct{}{}:
				default: // ein Signal genügt, es wird ohnehin zusammengefasst
				}
			}
		}
	}()
	return ch, nil
}
