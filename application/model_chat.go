package application

// ModelChat is one turn of a proxied model call.
type ModelChat struct {
	Role    string
	Content string
}
