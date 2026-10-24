package main

import (
	"net"
	"os"
	"strings"
)

// serverNames are the names and addresses the control plane's
// certificate is valid for: the listen address's host when it is one,
// every address of the host when it listens on all of them, the host
// name (long and short), the loopback, and whatever --server-names
// adds (the public name nodes on other hosts dial).
func serverNames(listen string, extra string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		host = listen
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if addrs, err := net.InterfaceAddrs(); err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLinkLocalUnicast() {
					add(ipNet.IP.String())
				}
			}
		}
	} else {
		add(host)
	}
	if hostName, err := os.Hostname(); err == nil {
		add(strings.ToLower(hostName))
		add(strings.ToLower(strings.SplitN(hostName, ".", 2)[0]))
	}
	for _, name := range []string{"localhost", "127.0.0.1", "::1"} {
		add(name)
	}
	for _, name := range strings.Split(extra, ",") {
		add(name)
	}
	return names
}
