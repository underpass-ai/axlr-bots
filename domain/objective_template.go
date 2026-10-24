package domain

import (
	"bytes"
	"errors"
	"text/template"
)

// ValidateObjectiveTemplate parses an objective template (text/template
// over the event, `{{.type}}`); it must not be empty.
func ValidateObjectiveTemplate(tpl string) error {
	if tpl == "" {
		return errors.New("must not be empty")
	}
	_, err := template.New("objective").Option("missingkey=error").Parse(tpl)
	return err
}

// RenderObjective renders the template over the event's fields into a
// valid objective. A field the template names and the event lacks is an
// error, so an objective never says `<no value>`.
func RenderObjective(tpl string, event map[string]any) (Objective, error) {
	parsed, err := template.New("objective").Option("missingkey=error").Parse(tpl)
	if err != nil {
		return "", invalid("objective template", err.Error())
	}
	var out bytes.Buffer
	if err := parsed.Execute(&out, event); err != nil {
		return "", invalid("objective template", err.Error())
	}
	return NewObjective(out.String())
}
