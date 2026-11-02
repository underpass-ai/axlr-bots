package unixapi

import (
	"net/http"
	"sync/atomic"
)

// liveClient is the HTTP client a Client sends through, replaceable
// while calls run: a node that renewed its certificate rebuilds its TLS
// client without stopping the heartbeat, the poll or a run's lease
// renewals, which all hold the same Client.
type liveClient struct {
	current atomic.Pointer[http.Client]
}

func newLiveClient(client *http.Client) *liveClient {
	live := &liveClient{}
	live.current.Store(client)
	return live
}

// Do sends the request through the client of the moment.
func (l *liveClient) Do(request *http.Request) (*http.Response, error) {
	return l.current.Load().Do(request)
}

// swap puts the client in place and returns the one it replaced.
func (l *liveClient) swap(client *http.Client) *http.Client {
	return l.current.Swap(client)
}
