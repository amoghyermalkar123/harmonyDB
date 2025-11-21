package debug

import "time"

// Config represents debug server configuration
type Config struct {
	Enabled      bool
	HTTPPort     int
	EnableRaft   bool
	EnableBTree  bool
	PollInterval time.Duration
}

// DefaultConfig returns default debug configuration
func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		HTTPPort:     6060,
		EnableRaft:   true,
		EnableBTree:  true,
		PollInterval: 500 * time.Millisecond,
	}
}
