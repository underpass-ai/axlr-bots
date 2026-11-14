package topicwire

// Message is one message as a bus adapter publishes it: the subject, the
// key JetStream drops a repeat by (MsgIDHeader) and the payload, a DTO
// as JSON.
type Message struct {
	Subject string
	MsgID   string
	Payload []byte
}
