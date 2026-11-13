package transcriptaudit

// Judgement is the judge's answer: the option chosen, the probability it
// gave hidden_error, and the input tokens the call cost.
type Judgement struct {
	Choice      string
	Probability float64
	InputTokens int
}
