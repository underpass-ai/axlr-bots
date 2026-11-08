package domain

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1" // #nosec G505 -- SNS SignatureVersion 1 is SHA1withRSA, AWS's own format.
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// The three kinds of message Amazon SNS posts to an HTTP/S subscription,
// named by its Type and by the x-amz-sns-message-type header.
const (
	SNSSubscriptionConfirmation = "SubscriptionConfirmation"
	SNSNotification             = "Notification"
	SNSUnsubscribeConfirmation  = "UnsubscribeConfirmation"
)

// snsHost is the host SNS signs from and confirms on: sns.<region>.amazonaws.com.
// The China partition (amazonaws.com.cn) is left out.
var snsHost = regexp.MustCompile(`^sns\.[a-z]{2}(-[a-z]+)+-[0-9]+\.amazonaws\.com$`)

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
	case !SNSURLAllowed(m.SigningCertURL):
		return SNSMessage{}, ForbiddenError{Reason: "the SigningCertURL is not on sns.<region>.amazonaws.com over HTTPS"}
	}
	if m.Type != SNSNotification {
		if m.Token == "" {
			return SNSMessage{}, invalid("Token", "a confirmation carries its token")
		}
		if !SNSURLAllowed(m.SubscribeURL) {
			return SNSMessage{}, ForbiddenError{Reason: "the SubscribeURL is not on sns.<region>.amazonaws.com over HTTPS"}
		}
	}
	return m, nil
}

// SNSURLAllowed reports whether a URL SNS gave is one the control plane
// may fetch: HTTPS, no credentials, no port, on sns.<region>.amazonaws.com.
// Anything else would let a forged body make the control plane fetch an
// address of the sender's choosing.
func SNSURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	return snsHost.MatchString(u.Hostname())
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
