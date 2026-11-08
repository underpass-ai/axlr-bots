package domain

import (
	"errors"
	"regexp"
	"slices"
)

// maxSNSTopics bounds a cloudwatch trigger's topic list.
const maxSNSTopics = 16

// snsTopicArn is an SNS topic's ARN in any partition but China's:
// arn:aws:sns:<region>:<account>:<name>.
var snsTopicArn = regexp.MustCompile(`^arn:aws(-[a-z]+)*:sns:[a-z]{2}(-[a-z]+)+-[0-9]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}(\.fifo)?$`)

// validateSNSTopics checks a cloudwatch trigger's topics: at most 16
// distinct SNS topic ARNs.
func validateSNSTopics(topics []string) ([]string, error) {
	if len(topics) > maxSNSTopics {
		return nil, errors.New("topics lists at most 16 SNS topic ARNs")
	}
	var out []string
	for _, topic := range topics {
		if !snsTopicArn.MatchString(topic) {
			return nil, errors.New("topic " + topic + " is not an SNS topic ARN (arn:aws:sns:<region>:<account>:<name>)")
		}
		if slices.Contains(out, topic) {
			return nil, errors.New("topics repeat " + topic)
		}
		out = append(out, topic)
	}
	return out, nil
}
