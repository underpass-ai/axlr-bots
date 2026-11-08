package domain

// OutputName names an output target, unique within the control plane.
type OutputName string

// NewOutputName validates an output's name.
func NewOutputName(raw string) (OutputName, error) {
	value, err := validateIdentifier("output name", raw)
	if err != nil {
		return "", err
	}
	return OutputName(value), nil
}

func (n OutputName) String() string { return string(n) }
