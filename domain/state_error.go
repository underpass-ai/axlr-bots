package domain

import "fmt"

// StateError says an aggregate refused a transition from its current state.
type StateError struct {
	Subject   string
	Current   string
	Attempted string
}

func (e StateError) Error() string {
	return fmt.Sprintf("%s is %s: cannot %s", e.Subject, e.Current, e.Attempted)
}
