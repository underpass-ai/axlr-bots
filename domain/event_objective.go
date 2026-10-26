package domain

// DefaultEventObjective is the objective of a task an event made, when
// the trigger that heard it names none (or the bot heard it by default):
// who said what, how serious, and the facts it gave.
const DefaultEventObjective = "Your teammate {{.from}} says ({{.severity}}, event {{.name}}): {{.message}}{{if .data}}\nThe facts it gave: {{.dataJSON}}{{end}}\nDo what your mission asks for this, now."
