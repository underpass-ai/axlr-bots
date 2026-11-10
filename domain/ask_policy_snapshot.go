package domain

// AskSnapshot is a bot's ask as plain data: the teams it may ask for
// context through the team channel, how many questions a minute and how
// many unanswered at once. Zero values mean "not given".
type AskSnapshot struct {
	Teams     []string `json:"teams,omitempty"`
	PerMinute int      `json:"perMinute,omitempty"`
	MaxOpen   int      `json:"maxOpen,omitempty"`
}
