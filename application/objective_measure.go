package application

import (
	"maps"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

// objectiveMeasure is what a template leaves of an objective for its
// long texts, beside the mission of the bot that will read it, and how
// many times the template writes each text.
type objectiveMeasure struct {
	room  int
	times map[string]int
}

// measureObjective renders the template with a holder in each of the
// named texts, so what a text brings with it is counted: a section under
// `{{if .draft}}`, the blank lines before a last field. A text the
// template only tests counts once, because it takes room to open its
// section.
func measureObjective(template string, fields map[string]any, named []string, mission domain.Mission) (objectiveMeasure, error) {
	measured := maps.Clone(fields)
	holders := make(map[string]string, len(named))
	for i, key := range named {
		holders[key] = "" + strconv.Itoa(i) + ""
		measured[key] = holders[key]
	}
	base, err := domain.RenderObjective(template, measured)
	if err != nil {
		return objectiveMeasure{}, err
	}
	fixed := len(base.String())
	times := make(map[string]int, len(named))
	for key, holder := range holders {
		written := strings.Count(base.String(), holder)
		fixed -= written * len(holder)
		times[key] = max(written, 1)
	}
	return objectiveMeasure{room: max(domain.ObjectiveRoomBeside(mission)-fixed, minPhaseObjectiveText), times: times}, nil
}

// each is the room one writing of the text has when all its writings
// share room.
func (m objectiveMeasure) each(key string, room int) int {
	return max(room/max(m.times[key], 1), minPhaseObjectiveText)
}
