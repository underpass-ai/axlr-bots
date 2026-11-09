package application

// JudgeVerdict is Jev's answer to a JudgeQuestion.
type JudgeVerdict struct {
	Choice      string
	Probability float64
	Confidence  float64
}
