//go:build windows

package app

import (
	"github.com/aiwaki/lumatape/internal/config"
	"github.com/aiwaki/lumatape/internal/platform/capture"
)

func transferName(t capture.Transfer) string {
	if t == capture.TransferCompatibility {
		return config.TransferCompatibility
	}
	return config.TransferGPU
}
func (a *application) resolvedConfig() config.Config {
	c := a.cfg
	if c.Capture.Transfer == config.TransferAuto {
		c.Capture.Transfer = a.resolvedTransfer
		if c.Capture.Transfer == "" {
			c.Capture.Transfer = config.TransferGPU
		}
	}
	return c
}
