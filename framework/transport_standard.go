//go:build !(linux || darwin || dragonfly || freebsd || netbsd || openbsd)

package framework

import (
	"time"

	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/network/standard"
)

func newDefaultTransport(time.Duration) network.Transport {
	return standard.New()
}
