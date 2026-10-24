package systemd

// Report says what an installation or a removal did and what to do next.
type Report struct {
	Mode      string   `json:"mode"`
	Binary    string   `json:"binary"`
	Units     []string `json:"units"`
	EnvFile   string   `json:"envFile"`
	StateDir  string   `json:"stateDir"`
	Socket    string   `json:"socket"`
	Commands  []string `json:"commands"`
	NextSteps []string `json:"nextSteps"`
}
