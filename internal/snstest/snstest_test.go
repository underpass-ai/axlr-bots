package snstest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

const body = `{"Type":"Notification","MessageId":"m-1","TopicArn":"arn:aws:sns:eu-west-1:123456789012:t","Message":"{}","Timestamp":"2026-11-08T10:00:00.000Z","SignatureVersion":"1","Signature":"x","SigningCertURL":"https://sns.eu-west-1.amazonaws.com/c.pem"}`

func TestSignedMessagesVerify(t *testing.T) {
	s, err := NewSigner(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1", "2"} {
		signed, err := s.Sign([]byte(body), func(m *domain.SNSMessage) { m.SignatureVersion = version })
		if err != nil {
			t.Fatal(err)
		}
		m, err := domain.ParseSNSMessage(signed)
		if err != nil || m.Verify(s.Certificate) != nil {
			t.Errorf("v%s: %v", version, err)
		}
	}
	if _, err := s.Sign([]byte("x"), nil); err == nil {
		t.Error("signed what is not JSON")
	}
	if len(s.PEM) == 0 {
		t.Error("no PEM")
	}
}

func TestFakeTransport(t *testing.T) {
	ctx := context.Background()
	transport := &Transport{}
	if _, err := transport.SigningCertificate(ctx, "a"); err == nil {
		t.Error("a certificate without a signer")
	}
	s, _ := NewSigner(time.Now())
	transport.Signer = s
	if c, err := transport.SigningCertificate(ctx, "b"); err != nil || c != s.Certificate || len(transport.Fetched()) != 2 {
		t.Errorf("certificate = %v %v", c, err)
	}
	if err := transport.ConfirmSubscription(ctx, "u"); err != nil || len(transport.Confirmed()) != 1 {
		t.Errorf("confirm = %v", err)
	}
	boom := errors.New("boom")
	transport.ConfirmFail = boom
	if err := transport.ConfirmSubscription(ctx, "u"); !errors.Is(err, boom) {
		t.Errorf("confirm fail = %v", err)
	}
	transport.Fail = boom
	if _, err := transport.SigningCertificate(ctx, "c"); !errors.Is(err, boom) {
		t.Errorf("fail = %v", err)
	}
	if err := transport.ConfirmSubscription(ctx, "u"); !errors.Is(err, boom) {
		t.Errorf("fail = %v", err)
	}
}
