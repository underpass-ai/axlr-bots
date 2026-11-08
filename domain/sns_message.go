package domain

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- SNS SignatureVersion 1 is SHA1withRSA, AWS's own format.
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// The three kinds of message Amazon SNS posts to an HTTP/S subscription,
// named by its Type and by the x-amz-sns-message-type header.
const (
	SNSSubscriptionConfirmation = "SubscriptionConfirmation"
	SNSNotification             = "Notification"
	SNSUnsubscribeConfirmation  = "UnsubscribeConfirmation"
	// SNSMaxMessageAge is how far a message's Timestamp may be from the
	// control plane's clock, either way: a signed message replayed later,
	// or one stamped in the future, is refused.
	SNSMaxMessageAge = 15 * time.Minute
)

// SNSMessage is the JSON body Amazon SNS posts to an HTTP/S endpoint: a
// notification (the published Message) or a subscription's confirmation
// or cancellation (SubscribeURL and Token), signed by SNS with the key
// of the certificate at SigningCertURL.
type SNSMessage struct {
	Type             string `json:"Type"`
	MessageID        string `json:"MessageId"`
	Token            string `json:"Token,omitempty"`
	TopicArn         string `json:"TopicArn"`
	Subject          string `json:"Subject,omitempty"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	SubscribeURL     string `json:"SubscribeURL,omitempty"`
	UnsubscribeURL   string `json:"UnsubscribeURL,omitempty"`
}

// ParseSNSMessage reads an SNS body and checks its shape: a known type,
// the fields its signature covers, a signature version of 1 or 2, and a
// signing certificate (and, for a confirmation, a subscribe URL) on an
// SNS host over HTTPS. It does not verify the signature: that needs the
// certificate (Verify).
func ParseSNSMessage(body []byte) (SNSMessage, error) {
	var m SNSMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return SNSMessage{}, invalid("body", "is not an Amazon SNS message: "+err.Error())
	}
	switch m.Type {
	case SNSNotification, SNSSubscriptionConfirmation, SNSUnsubscribeConfirmation:
	default:
		return SNSMessage{}, invalid("Type", "must be Notification, SubscriptionConfirmation or UnsubscribeConfirmation")
	}
	switch {
	case m.MessageID == "" || m.TopicArn == "" || m.Timestamp == "":
		return SNSMessage{}, invalid("body", "an SNS message names its MessageId, TopicArn and Timestamp")
	case m.SignatureVersion != "1" && m.SignatureVersion != "2":
		return SNSMessage{}, invalid("SignatureVersion", "must be 1 or 2")
	case m.Signature == "":
		return SNSMessage{}, invalid("Signature", "must not be empty")
	}
	if _, err := SNSCertificateURL(m.SigningCertURL); err != nil {
		return SNSMessage{}, err
	}
	if m.Type != SNSNotification {
		if m.Token == "" {
			return SNSMessage{}, invalid("Token", "a confirmation carries its token")
		}
		if _, err := SNSSubscribeURL(m.SubscribeURL); err != nil {
			return SNSMessage{}, err
		}
	}
	return m, nil
}

// StringToSign is what SNS signed, as AWS documents it: the covered
// fields, by name in byte order, each as "name\nvalue\n". A notification
// covers Message, MessageId, Subject (only when present), Timestamp,
// TopicArn and Type; a confirmation Message, MessageId, SubscribeURL,
// Timestamp, Token, TopicArn and Type.
func (m SNSMessage) StringToSign() string {
	var fields [][2]string
	if m.Type == SNSNotification {
		fields = append(fields, [2]string{"Message", m.Message}, [2]string{"MessageId", m.MessageID})
		if m.Subject != "" {
			fields = append(fields, [2]string{"Subject", m.Subject})
		}
		fields = append(fields, [2]string{"Timestamp", m.Timestamp}, [2]string{"TopicArn", m.TopicArn}, [2]string{"Type", m.Type})
	} else {
		fields = [][2]string{
			{"Message", m.Message}, {"MessageId", m.MessageID}, {"SubscribeURL", m.SubscribeURL},
			{"Timestamp", m.Timestamp}, {"Token", m.Token}, {"TopicArn", m.TopicArn}, {"Type", m.Type},
		}
	}
	var out strings.Builder
	for _, field := range fields {
		out.WriteString(field[0])
		out.WriteByte('\n')
		out.WriteString(field[1])
		out.WriteByte('\n')
	}
	return out.String()
}

// CheckFresh refuses a message whose Timestamp is more than
// SNSMaxMessageAge from now, before or after, or is not a time: a signed
// message stays valid for good, and its Timestamp, which the signature
// covers, is what bounds a replay. A refusal is a ForbiddenError.
func (m SNSMessage) CheckFresh(now time.Time) error {
	at, err := time.Parse(time.RFC3339Nano, m.Timestamp)
	if err != nil {
		return ForbiddenError{Reason: "the SNS message's Timestamp is not a time"}
	}
	if age := now.Sub(at); age > SNSMaxMessageAge || age < -SNSMaxMessageAge {
		return ForbiddenError{Reason: "the SNS message's Timestamp " + m.Timestamp + " is more than 15 minutes from now: a replay, or a clock out of step"}
	}
	return nil
}

// Verify checks the message's signature against the signing
// certificate's RSA key: SHA1withRSA for version 1, SHA256withRSA for
// version 2. A refusal is a ForbiddenError.
func (m SNSMessage) Verify(certificate *x509.Certificate) error {
	refused := ForbiddenError{Reason: "the SNS message's signature does not verify"}
	if certificate == nil {
		return refused
	}
	key, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return ForbiddenError{Reason: "the SNS signing certificate has no RSA key"}
	}
	signature, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		return refused
	}
	signed := []byte(m.StringToSign())
	var hash crypto.Hash
	var digest []byte
	if m.SignatureVersion == "1" {
		sum := sha1.Sum(signed) // #nosec G401 -- AWS's SignatureVersion 1.
		hash, digest = crypto.SHA1, sum[:]
	} else {
		sum := sha256.Sum256(signed)
		hash, digest = crypto.SHA256, sum[:]
	}
	if err := rsa.VerifyPKCS1v15(key, hash, digest, signature); err != nil {
		return refused
	}
	return nil
}
