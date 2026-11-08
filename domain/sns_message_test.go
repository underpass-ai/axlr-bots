package domain_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/snstest"
)

// The bodies under testdata/sns-* are composed from AWS's documented
// formats (SNS's HTTP/S JSON and CloudWatch's alarm message), not
// captured from a real topic: their signatures are placeholders, and
// each test re-signs them with a key it makes (snstest).

func snsBody(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "sns-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func signer(t *testing.T) *snstest.Signer {
	t.Helper()
	s, err := snstest.NewSigner(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSNSStringToSignFollowsAWSOrder(t *testing.T) {
	m, err := domain.ParseSNSMessage(snsBody(t, "cloudwatch-alarm"))
	if err != nil {
		t.Fatal(err)
	}
	got := m.StringToSign()
	want := "Message\n" + m.Message + "\nMessageId\n5f1d0c2e-8a7b-5c3d-9e4f-0a1b2c3d4e5f\nSubject\nALARM: \"shop-web-5xx\" in EU (Ireland)\nTimestamp\n2026-11-08T10:03:12.455Z\nTopicArn\narn:aws:sns:eu-west-1:123456789012:axlr-sre-alarms\nType\nNotification\n"
	if got != want {
		t.Errorf("string to sign =\n%q\nwant\n%q", got, want)
	}
	m.Subject = ""
	if strings.Contains(m.StringToSign(), "Subject") {
		t.Error("an absent Subject is signed")
	}
	c, err := domain.ParseSNSMessage(snsBody(t, "subscription-confirmation"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n, line := range strings.Split(strings.TrimSuffix(c.StringToSign(), "\n"), "\n") {
		if n%2 == 0 {
			names = append(names, line)
		}
	}
	// The Message has a newline of its own: the names are every other
	// line up to it, then shifted by one.
	if joined := strings.Join(names, ","); !strings.HasPrefix(joined, "Message,") || !strings.Contains(c.StringToSign(), "\nSubscribeURL\nhttps://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription") || !strings.HasSuffix(c.StringToSign(), "\nToken\n"+c.Token+"\nTopicArn\n"+c.TopicArn+"\nType\nSubscriptionConfirmation\n") {
		t.Errorf("confirmation string to sign = %q", c.StringToSign())
	}
}

func TestSNSSignatureVersionsOneAndTwo(t *testing.T) {
	s := signer(t)
	for _, name := range []string{"cloudwatch-alarm", "cloudwatch-ok", "subscription-confirmation", "unsubscribe-confirmation"} {
		for _, version := range []string{"1", "2"} {
			body, err := s.Sign(snsBody(t, name), func(m *domain.SNSMessage) { m.SignatureVersion = version })
			if err != nil {
				t.Fatal(err)
			}
			m, err := domain.ParseSNSMessage(body)
			if err != nil {
				t.Fatalf("%s v%s: %v", name, version, err)
			}
			if err := m.Verify(s.Certificate); err != nil {
				t.Errorf("%s v%s: %v", name, version, err)
			}
			tampered := m
			tampered.Message += " "
			if err := tampered.Verify(s.Certificate); err == nil {
				t.Errorf("%s v%s: a changed message verified", name, version)
			}
			other := m
			other.SignatureVersion = map[string]string{"1": "2", "2": "1"}[version]
			if err := other.Verify(s.Certificate); err == nil {
				t.Errorf("%s v%s: verified under the other version", name, version)
			}
		}
	}
}

func TestSNSSignatureRefusals(t *testing.T) {
	s, stranger := signer(t), signer(t)
	body, err := s.Sign(snsBody(t, "cloudwatch-alarm"), nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := domain.ParseSNSMessage(body)
	if err != nil {
		t.Fatal(err)
	}
	var forbidden domain.ForbiddenError
	if err := m.Verify(stranger.Certificate); !errors.As(err, &forbidden) {
		t.Errorf("another key: %v", err)
	}
	if err := m.Verify(nil); !errors.As(err, &forbidden) {
		t.Errorf("no certificate: %v", err)
	}
	recorded, err := domain.ParseSNSMessage(snsBody(t, "cloudwatch-alarm"))
	if err != nil {
		t.Fatal(err)
	}
	if err := recorded.Verify(s.Certificate); !errors.As(err, &forbidden) {
		t.Errorf("the placeholder signature: %v", err)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err := m.Verify(&x509.Certificate{PublicKey: &key.PublicKey}); !errors.As(err, &forbidden) || !strings.Contains(err.Error(), "RSA") {
		t.Errorf("an EC certificate: %v", err)
	}
}

func TestParseSNSMessageRefusesWhatIsNotSNS(t *testing.T) {
	good := string(snsBody(t, "cloudwatch-alarm"))
	confirmation := string(snsBody(t, "subscription-confirmation"))
	cert := "https://sns.eu-west-1.amazonaws.com/SimpleNotificationService-9c6465fa7f48f5cacd23014631ec1136.pem"
	cases := map[string]string{
		"not json":       "Type=Notification",
		"unknown type":   strings.Replace(good, `"Type": "Notification"`, `"Type": "Publish"`, 1),
		"no message id":  strings.Replace(good, `"MessageId": "5f1d0c2e-8a7b-5c3d-9e4f-0a1b2c3d4e5f"`, `"MessageId": ""`, 1),
		"version 3":      strings.Replace(good, `"SignatureVersion": "1"`, `"SignatureVersion": "3"`, 1),
		"no signature":   strings.Replace(good, `"Signature": "EXAMPLEpH+DcEwjAPg8O9mY8dReBSwksfg2S7WKQcikcNKWLQjwu6A4VbeS0QHVCkhRS7fUQvi2egU3N858fiTDN6bkkOxYDVrY0Ad8L10Hs3zH81mtnPk5uvvolIC1CXGu43obcgFxeL3khZl8IKvO61GWB6jI9b5+gLPoBc1Q="`, `"Signature": ""`, 1),
		"confirm no tok": strings.Replace(confirmation, `"Token": "2336412f37fb687f5d51e6e2425f004aed3d0ea5d8a5c8b1f0b2a1e9c0d7f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1"`, `"Token": ""`, 1),
	}
	for name, body := range cases {
		if _, err := domain.ParseSNSMessage([]byte(body)); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	var forbidden domain.ForbiddenError
	for _, host := range []string{"http://sns.eu-west-1.amazonaws.com/x.pem", "https://sns.eu-west-1.amazonaws.com.evil.example/x.pem", "https://evil.example/x.pem", "https://sns.eu-west-1.amazonaws.com:8443/x.pem", "https://user@sns.eu-west-1.amazonaws.com/x.pem", "https://s3.eu-west-1.amazonaws.com/x.pem", "::"} {
		body := strings.Replace(good, cert, host, 1)
		if _, err := domain.ParseSNSMessage([]byte(body)); !errors.As(err, &forbidden) {
			t.Errorf("cert at %s: %v", host, err)
		}
	}
	subscribe := strings.Replace(confirmation, "https://sns.eu-west-1.amazonaws.com/?Action=ConfirmSubscription", "https://169.254.169.254/?Action=ConfirmSubscription", 1)
	if _, err := domain.ParseSNSMessage([]byte(subscribe)); !errors.As(err, &forbidden) {
		t.Errorf("subscribe URL off SNS: %v", err)
	}
	for _, allowed := range []string{"https://sns.us-east-1.amazonaws.com/a.pem", "https://sns.us-gov-west-1.amazonaws.com/a.pem", "https://sns.ap-southeast-5.amazonaws.com/a.pem"} {
		if !domain.SNSURLAllowed(allowed) {
			t.Errorf("%s refused", allowed)
		}
	}
}

func TestCloudWatchAlarmChangeMapping(t *testing.T) {
	m, err := domain.ParseSNSMessage(snsBody(t, "cloudwatch-alarm"))
	if err != nil {
		t.Fatal(err)
	}
	change, err := domain.ParseCloudWatchAlarmChange(m.Message)
	if err != nil {
		t.Fatal(err)
	}
	if change.GroupKey() != "cloudwatch:arn:aws:cloudwatch:eu-west-1:123456789012:alarm:shop-web-5xx" || change.Fingerprint() != "2026-11-08T10:03:12.402+0000" || change.NewStateValue != domain.CloudWatchAlarm {
		t.Errorf("change = %s %s %s", change.GroupKey(), change.Fingerprint(), change.NewStateValue)
	}
	if title := change.Title(); len(title) > 200 || !strings.HasPrefix(title, "shop-web-5xx: Threshold Crossed: 3 out of the last 3 datapoints") || !strings.HasSuffix(title, "…") {
		t.Errorf("title = %q (%d)", title, len(title))
	}
	fields := change.Fields()
	if fields["alarmName"] != "shop-web-5xx" || !strings.HasPrefix(fields["reason"].(string), "Threshold Crossed") || fields["alarm"].(map[string]any)["Region"] != "EU (Ireland)" {
		t.Errorf("fields = %v", fields)
	}
	objective, err := domain.RenderObjective("Alarm {{.alarmName}}: {{.reason}} ({{.alarm.Trigger.MetricName}})", fields)
	if err != nil || !strings.Contains(objective.String(), "HTTPCode_Target_5XX_Count") {
		t.Errorf("objective = %q %v", objective, err)
	}
	short := domain.CloudWatchAlarmChange{AlarmName: "db", NewStateReason: "line one\nline two"}
	if short.Title() != "db: line one line two" {
		t.Errorf("short title = %q", short.Title())
	}
	if (domain.CloudWatchAlarmChange{AlarmName: "x"}).Title() != "x" {
		t.Error("a title without a reason")
	}
	byName := domain.CloudWatchAlarmChange{AlarmName: "orders-latency"}
	if byName.GroupKey() != "cloudwatch:orders-latency" {
		t.Errorf("by name = %s", byName.GroupKey())
	}
	long := domain.CloudWatchAlarmChange{AlarmName: strings.Repeat("a", 255), AlarmArn: "arn:aws:cloudwatch:eu-west-1:123456789012:alarm:" + strings.Repeat("a", 255)}
	if key := long.GroupKey(); !strings.HasPrefix(key, "cloudwatch:sha256:") || len(key) > 256 || key != (domain.CloudWatchAlarmChange{AlarmArn: long.AlarmArn}).GroupKey() {
		t.Errorf("long key = %s", key)
	}
}

func TestParseCloudWatchAlarmChangeRefusals(t *testing.T) {
	for name, message := range map[string]string{
		"not json":   "ALARM",
		"no name":    `{"NewStateValue":"ALARM","StateChangeTime":"2026-11-08T10:00:00.000+0000"}`,
		"no time":    `{"AlarmName":"a","NewStateValue":"ALARM"}`,
		"long time":  `{"AlarmName":"a","NewStateValue":"ALARM","StateChangeTime":"` + strings.Repeat("1", 200) + `"}`,
		"bad state":  `{"AlarmName":"a","NewStateValue":"RED","StateChangeTime":"2026-11-08T10:00:00.000+0000"}`,
		"json array": `[]`,
	} {
		if _, err := domain.ParseCloudWatchAlarmChange(message); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
}

func TestSNSMessageFreshness(t *testing.T) {
	m, err := domain.ParseSNSMessage(snsBody(t, "cloudwatch-alarm"))
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339Nano, m.Timestamp)
	if err != nil {
		t.Fatal(err)
	}
	for name, now := range map[string]time.Time{
		"now":           at,
		"15 min later":  at.Add(domain.SNSMaxMessageAge),
		"15 min before": at.Add(-domain.SNSMaxMessageAge),
	} {
		if err := m.CheckFresh(now); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	var forbidden domain.ForbiddenError
	for name, now := range map[string]time.Time{
		"replayed":  at.Add(domain.SNSMaxMessageAge + time.Second),
		"in future": at.Add(-domain.SNSMaxMessageAge - time.Second),
	} {
		if err := m.CheckFresh(now); !errors.As(err, &forbidden) || !strings.Contains(err.Error(), "15 minutes") {
			t.Errorf("%s: %v", name, err)
		}
	}
	m.Timestamp = "yesterday"
	if err := m.CheckFresh(at); !errors.As(err, &forbidden) {
		t.Errorf("not a time: %v", err)
	}
}
