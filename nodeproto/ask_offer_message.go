package nodeproto

// AskOfferMessage tells the node its execution's bot may ask these other
// teams; an older node ignores it and serves no ask tool.
type AskOfferMessage struct {
	Teams []string `json:"teams"`
}
