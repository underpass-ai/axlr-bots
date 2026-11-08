package domain

import (
	"net/url"
	"regexp"
)

// The shapes of the URLs SNS puts in a message, read before anything is
// fetched: the host it signs from and confirms on (the China partition,
// amazonaws.com.cn, is left out), the path of a signing certificate, and
// the token of a confirmation.
var (
	snsHost            = regexp.MustCompile(`^sns\.[a-z]{2}(-[a-z]+)+-[0-9]+\.amazonaws\.com$`)
	snsCertificatePath = regexp.MustCompile(`^/SimpleNotificationService-[A-Za-z0-9]{1,128}\.pem$`)
	snsToken           = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,1000}$`)
)

const snsConfirmAction = "ConfirmSubscription"

// SNSCertificateURL is the URL to fetch a message's signing certificate
// from: https://sns.<region>.amazonaws.com/SimpleNotificationService-<id>.pem,
// and nothing else. A refusal is a ForbiddenError. The answer is built
// from the parts checked here, never from the text given, so a forged
// body cannot make the control plane fetch an address of its choosing.
func SNSCertificateURL(raw string) (string, error) {
	parsed, err := parseSNSURL(raw, "SigningCertURL")
	if err != nil {
		return "", err
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return "", snsURLRefused("SigningCertURL")
	}
	host, path := parsed.Hostname(), parsed.EscapedPath()
	if !snsHost.MatchString(host) || !snsCertificatePath.MatchString(path) {
		return "", snsURLRefused("SigningCertURL")
	}
	return "https://" + host + path, nil
}

// SNSSubscribeURL is the URL that confirms a subscription:
// https://sns.<region>.amazonaws.com/?Action=ConfirmSubscription&TopicArn=<arn>&Token=<token>,
// and nothing else. A refusal is a ForbiddenError. Like
// SNSCertificateURL, the answer is built from the checked parts.
func SNSSubscribeURL(raw string) (string, error) {
	parsed, err := parseSNSURL(raw, "SubscribeURL")
	if err != nil {
		return "", err
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || len(query) != 3 || len(query["Action"]) != 1 || len(query["TopicArn"]) != 1 || len(query["Token"]) != 1 {
		return "", snsURLRefused("SubscribeURL")
	}
	host, path, topic, token := parsed.Hostname(), parsed.EscapedPath(), query.Get("TopicArn"), query.Get("Token")
	if query.Get("Action") != snsConfirmAction || (path != "" && path != "/") {
		return "", snsURLRefused("SubscribeURL")
	}
	if !snsHost.MatchString(host) || !snsTopicArn.MatchString(topic) || !snsToken.MatchString(token) {
		return "", snsURLRefused("SubscribeURL")
	}
	return "https://" + host + "/?Action=" + snsConfirmAction + "&TopicArn=" + topic + "&Token=" + token, nil
}

// parseSNSURL reads a URL of a message and refuses what no URL of SNS's
// has: another scheme, credentials, a port, a fragment.
func parseSNSURL(raw, field string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return nil, snsURLRefused(field)
	}
	return parsed, nil
}

func snsURLRefused(field string) error {
	return ForbiddenError{Reason: "the " + field + " is not one of Amazon SNS's: https on sns.<region>.amazonaws.com, in the shape SNS gives it"}
}
