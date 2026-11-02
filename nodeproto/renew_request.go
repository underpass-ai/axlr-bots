package nodeproto

// RenewRequest is what an enrolled node presents, over mutual TLS with
// its current certificate, to trade that certificate for a new one
// before it expires: a signing request in PEM for a key it drew afresh.
// The answer is an EnrollResponse.
type RenewRequest struct {
	CSR string `json:"csr"`
}
