package domain

// EnrollmentID identifies a one-time enrollment token; the secret that
// goes with it is never stored, only its hash.
type EnrollmentID string

// NewEnrollmentID validates an enrollment identifier.
func NewEnrollmentID(raw string) (EnrollmentID, error) {
	value, err := validateIdentifier("enrollment id", raw)
	if err != nil {
		return "", err
	}
	return EnrollmentID(value), nil
}

func (id EnrollmentID) String() string { return string(id) }
