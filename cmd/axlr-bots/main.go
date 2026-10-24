// Command axlr-bots is the stub control plane of AXLR Bots:
// it registers bots, accepts tasks and runs them through AXLR, keeping
// everything under a state directory so a bot survives a restart.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
