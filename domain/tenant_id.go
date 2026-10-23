package domain

// TenantID identifies a tenant.
type TenantID string

// NewTenantID validates a tenant identifier.
func NewTenantID(raw string) (TenantID, error) {
	value, err := validateIdentifier("tenant id", raw)
	if err != nil {
		return "", err
	}
	return TenantID(value), nil
}

func (id TenantID) String() string { return string(id) }
