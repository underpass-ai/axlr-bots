package application

// PostmortemReview is the send-back a rewrite answers: the attempt it
// starts (2 for the first rewrite), the person's note and who wrote it,
// and the draft they sent back, when there was one.
type PostmortemReview struct {
	Attempt int    `json:"attempt"`
	Note    string `json:"note"`
	By      string `json:"by,omitempty"`
	Draft   string `json:"draft,omitempty"`
}
