package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/domain"
)

const (
	certPath = "/SimpleNotificationService-9c6465fa.pem"
	snsSite  = "sns.eu-west-1.amazonaws.com"
	confirm  = "/?Action=ConfirmSubscription&TopicArn=arn:aws:sns:eu-west-1:123456789012:alarms&Token=2336412f"
)

func TestSNSURLsAreRebuiltFromWhatWasChecked(t *testing.T) {
	if got, err := domain.SNSCertificateURL("https://" + snsSite + certPath); err != nil || got != "https://"+snsSite+certPath {
		t.Errorf("certificate = %q %v", got, err)
	}
	if got, err := domain.SNSSubscribeURL("https://" + snsSite + confirm); err != nil || got != "https://"+snsSite+confirm {
		t.Errorf("subscribe = %q %v", got, err)
	}
	// The order of the query is not the rebuilt one's, and an encoded ARN reads the same.
	shuffled := "https://" + snsSite + "?Token=2336412f&TopicArn=arn%3Aaws%3Asns%3Aeu-west-1%3A123456789012%3Aalarms&Action=ConfirmSubscription"
	if got, err := domain.SNSSubscribeURL(shuffled); err != nil || got != "https://"+snsSite+confirm {
		t.Errorf("shuffled = %q %v", got, err)
	}
}

func TestSNSURLsRefuseWhatSNSNeverSends(t *testing.T) {
	hosts := map[string]string{
		"http":             "http://" + snsSite,
		"look-alike":       "https://" + snsSite + ".evil.example",
		"another host":     "https://evil.example",
		"another service":  "https://s3.eu-west-1.amazonaws.com",
		"no region":        "https://sns.amazonaws.com",
		"user info":        "https://user@" + snsSite,
		"host as user":     "https://" + snsSite + "@evil.example",
		"port":             "https://" + snsSite + ":8443",
		"metadata address": "https://169.254.169.254",
		"loopback":         "https://127.0.0.1",
		"ipv6 literal":     "https://[::1]",
		"trailing dot":     "https://" + snsSite + ".",
		"upper case":       "https://SNS.EU-WEST-1.AMAZONAWS.COM",
		"opaque":           "https:" + snsSite,
		"not a url":        "::",
	}
	var forbidden domain.ForbiddenError
	for name, base := range hosts {
		if _, err := domain.SNSCertificateURL(base + certPath); !errors.As(err, &forbidden) {
			t.Errorf("certificate, %s: %v", name, err)
		}
		if _, err := domain.SNSSubscribeURL(base + confirm); !errors.As(err, &forbidden) {
			t.Errorf("subscribe, %s: %v", name, err)
		}
	}
	base := "https://" + snsSite
	for name, url := range map[string]string{
		"another file":  base + "/x.pem",
		"traversal":     base + "/../SimpleNotificationService-1.pem",
		"encoded slash": base + "/%2e%2e/SimpleNotificationService-1.pem",
		"nested":        base + "/a" + certPath,
		"query":         base + certPath + "?x=1",
		"empty query":   base + certPath + "?",
		"fragment":      base + certPath + "#x",
		"empty":         "",
	} {
		if _, err := domain.SNSCertificateURL(url); !errors.As(err, &forbidden) {
			t.Errorf("certificate, %s: %v", name, err)
		}
	}
	topic := "TopicArn=arn:aws:sns:eu-west-1:123456789012:alarms"
	for name, url := range map[string]string{
		"no query":       base + "/",
		"another action": base + "/?Action=Unsubscribe&" + topic + "&Token=a",
		"no token":       base + "/?Action=ConfirmSubscription&" + topic,
		"no topic":       base + "/?Action=ConfirmSubscription&Token=a",
		"a fourth":       base + "/?Action=ConfirmSubscription&" + topic + "&Token=a&Next=https://evil.example",
		"twice":          base + "/?Action=ConfirmSubscription&" + topic + "&Token=a&Token=b",
		"bad topic":      base + "/?Action=ConfirmSubscription&TopicArn=x&Token=a",
		"bad token":      base + "/?Action=ConfirmSubscription&" + topic + "&Token=a%26b",
		"long token":     base + "/?Action=ConfirmSubscription&" + topic + "&Token=" + strings.Repeat("a", 1001),
		"a path":         base + "/latest/?Action=ConfirmSubscription&" + topic + "&Token=a",
		"bad escape":     base + "/?Action=ConfirmSubscription&" + topic + "&Token=%zz",
		"fragment":       base + "/?Action=ConfirmSubscription&" + topic + "&Token=a#x",
	} {
		if _, err := domain.SNSSubscribeURL(url); !errors.As(err, &forbidden) {
			t.Errorf("subscribe, %s: %v", name, err)
		}
	}
}
