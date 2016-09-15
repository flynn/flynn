package bootstrap

import (
	"encoding/base64"
	"fmt"

	"github.com/flynn/flynn/pkg/random"
)

type GenRandomAction struct {
	ID       string `json:"id"`
	Length   int    `json:"length"`
	Data     string `json:"data"`
	Encoding string `json:"encoding"`

	ControllerKey bool `json:"controller_key"`
}

func init() {
	Register("gen-random", &GenRandomAction{})
}

type RandomData struct {
	Data string `json:"data"`
}

func (d *RandomData) String() string {
	return d.Data
}

func (a *GenRandomAction) Run(s *State) error {
	if a.Length == 0 {
		a.Length = 16
	}
	data := interpolate(s, a.Data)
	if data == "" {
		switch a.Encoding {
		case "", "hex":
			data = random.Hex(a.Length)
		case "base64":
			data = base64.StdEncoding.EncodeToString(random.Bytes(a.Length))
		case "base64safe":
			data = random.Base64(a.Length)
		case "uuid":
			data = random.UUID()
		default:
			return fmt.Errorf("bootstrap: unknown random type: %q", a.Encoding)
		}
	}
	s.StepData[a.ID] = &RandomData{Data: data}
	if a.ControllerKey {
		s.SetControllerKey(data)
	}
	return nil
}
