package systemd

// Report says what an installation or a removal did and what to do next.
type Report struct {
	Mode         string   `json:"mode"`
	Binary       string   `json:"binary,omitempty"`
	NodeBinary   string   `json:"nodeBinary"`
	Units        []string `json:"units"`
	EnvFile      string   `json:"envFile"`
	StateDir     string   `json:"stateDir,omitempty"`
	NodeStateDir string   `json:"nodeStateDir"`
	Socket       string   `json:"socket,omitempty"`
	Control      string   `json:"control,omitempty"`
	Listen       string   `json:"listen,omitempty"`
	Commands     []string `json:"commands"`
	NextSteps    []string `json:"nextSteps"`
}
