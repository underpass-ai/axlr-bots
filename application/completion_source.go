package application

// CompletionSource says where the items after a cut came from.
type CompletionSource string

// The sources of a completion.
const (
	// CompletionNone: nothing was added after the cut.
	CompletionNone CompletionSource = "none"
	// CompletionTranscript: items were read from the execution's transcript.
	CompletionTranscript CompletionSource = "transcript"
)
