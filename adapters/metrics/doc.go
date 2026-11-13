// Package metrics exposes the control plane to Prometheus: the counts
// of the overview read at each scrape, the requests each listener
// serves, and the Go runtime and process. Nothing outside this adapter
// imports the Prometheus client; the application knows nothing of it.
package metrics
