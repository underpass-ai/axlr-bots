package application

// EnrollmentRequest is what a node presents to enroll: the token's id
// and secret, its host name and its certificate signing request.
type EnrollmentRequest struct {
	TokenID  string
	Secret   string
	HostName string
	CSRPEM   []byte
}
