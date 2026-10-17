//go:build darwin && devicebuttonsacceptance

package macos

type MouseInterceptionEvidence struct {
	Enabled      bool `json:"enabled"`
	Pending      int  `json:"pending"`
	ConsumedDown int  `json:"consumedDown"`
	ConsumedUp   int  `json:"consumedUp"`
}

func MouseButtonInterceptionEvidence() (out MouseInterceptionEvidence) {
	OnMain(func() {
		out.Enabled = deviceButtonsTap.tap != 0 && cgEventTapIsEnabled(deviceButtonsTap.tap)
		out.Pending = len(deviceButtonsTap.held)
		out.ConsumedDown, out.ConsumedUp = deviceButtonsTap.consumedDown, deviceButtonsTap.consumedUp
	})
	return
}
