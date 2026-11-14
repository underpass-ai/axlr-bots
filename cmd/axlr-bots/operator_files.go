package main

import (
	"os"
	"path/filepath"
)

// Files of an operator's credentials, which `ca operator issue` writes
// and the CLI and the panel look for.
const (
	operatorKeyFile  = "operator.key"
	operatorCertFile = "operator.crt"
	operatorCAFile   = "ca.crt"
)

// defaultOperatorDir is where `ca operator issue` leaves the local
// operator's certificate and key: <user config>/axlr-bots/operator
// (~/.config/axlr-bots/operator on Linux).
func defaultOperatorDir() string {
	if dir := os.Getenv("AXLR_BOTS_OPERATOR_DIR"); dir != "" {
		return dir
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(".", "axlr-bots-operator")
	}
	return filepath.Join(config, "axlr-bots", "operator")
}
