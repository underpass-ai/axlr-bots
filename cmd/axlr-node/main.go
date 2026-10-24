// Command axlr-node is the node agent of AXLR Bots: the process
// a host runs under systemd. It registers with the control plane, polls
// for the executions of the bots placed on it, runs them with AXLR on
// the host and reports the results. Every connection goes out from the
// node to the control plane.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
