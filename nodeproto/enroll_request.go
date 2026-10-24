package nodeproto

// EnrollRequest is what a node presents once to be enrolled: the token
// (its id and secret), its host name and its certificate signing request
// in PEM. It travels over TLS without a client certificate, the only
// node message that does.
type EnrollRequest struct {
	TokenID  string `json:"tokenId"`
	Secret   string `json:"secret"`
	HostName string `json:"hostName"`
	CSR      string `json:"csr"`
}
