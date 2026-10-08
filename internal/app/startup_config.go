package app

import (
	"errors"
	"os"

	"github.com/aiwaki/lumatape/internal/config"
)

// A new desktop user chooses a source and applies settings before the real
// filter runs. Existing profiles, including ones with omitted fields, retain
// their saved/default interpretation. Native CLI first-launch defaults remain
// unchanged. The startup caller persists this initial profile in its normal path.
func loadStartupConfig(path string, controlled bool) (config.Config, error) {
	c, err := config.LoadExisting(path)
	if errors.Is(err, os.ErrNotExist) {
		c = config.Default()
		if controlled {
			c.Enabled = false
			c.Capture.Transfer = config.TransferAuto
			c.Hotkeys = config.Hotkeys{Toggle: "Ctrl+Shift+9", Emergency: "Ctrl+Shift+0"}
		}
		return c, nil
	}
	return c, err
}
