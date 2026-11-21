//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package framework

import (
	"time"

	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/network/netpoll"
)

func newDefaultTransport(idleTimeout time.Duration) network.Transport {
	return netpoll.New(netpoll.WithIdleTimeout(idleTimeout))
}
