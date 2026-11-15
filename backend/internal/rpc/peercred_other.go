//go:build !linux

package rpc

import "net"

// peerUID ist außerhalb von Linux nicht verfügbar; erlaubt nur Entwicklung auf macOS.
// Ein Server mit UID-Liste lehnt dort jede Verbindung ab.
func peerUID(net.Conn) (uint32, error) { return ^uint32(0), nil }
