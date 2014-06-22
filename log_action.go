package bootstrap

type LogAction struct {
	ID     string `json:"id"`
	Output string `json:"output"`
}

type LogMessage struct {
	Msg string `json:"message"`
}

func (l *LogMessage) String() string {
	return l.Msg
}

func init() {
	Register("log", &LogAction{})
}

func (a *LogAction) Run(s *State) error {
	s.StepData[a.ID] = &LogMessage{Msg: interpolate(s, a.Output)}
	return nil
}
