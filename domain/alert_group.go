package domain

// AlertGroup says what is known of an incident's alerts: empty when
// nobody said (a hand-opened incident), one alert, or a mixed group.
type AlertGroup string

const (
	AlertGroupUnknown AlertGroup = ""
	AlertGroupUniform AlertGroup = "uniform"
	AlertGroupMixed   AlertGroup = "mixed"
)

// ParseAlertGroup validates a stored value.
func ParseAlertGroup(raw string) (AlertGroup, error) {
	switch AlertGroup(raw) {
	case AlertGroupUnknown, AlertGroupUniform, AlertGroupMixed:
		return AlertGroup(raw), nil
	}
	return "", invalid("alert group", "must be uniform or mixed")
}

func (g AlertGroup) String() string { return string(g) }
