package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

type GenRandomAction struct {
	ID     string `json:"id"`
	Length int    `json:"length"`
	Data   string `json:"data"`

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
		data = randomData(a.Length)
	}
	s.StepData[a.ID] = &RandomData{Data: data}
	if a.ControllerKey {
		s.SetControllerKey(data)
	}
	return nil
}

func randomData(n int) string {
	data := make([]byte, n)
	_, err := io.ReadFull(rand.Reader, data)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(data)
}
