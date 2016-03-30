package installer

import (
	"encoding/json"
	"io"

	"github.com/flynn/flynn/pkg/random"
)

type PromptType string

var (
	PromptTypeYesNo          PromptType = "yes_no"
	PromptTypeChoice         PromptType = "choice"
	PromptTypeCredential     PromptType = "credential"
	PromptTypeInput          PromptType = "input"
	PromptTypeProtectedInput PromptType = "protected_input"
	PromptTypeFile           PromptType = "file"
)

type ChoicePrompt struct {
	Options []ChoicePromptOption `json:"options"`
}

type ChoicePromptOption struct {
	Type  ChoicePromptOptionType `json:"type"`
	Name  string                 `json:"name"`
	Value string                 `json:"value"`
}

type ChoicePromptOptionType string

var (
	ChoicePromptOptionTypePrimary ChoicePromptOptionType = "primary"
	ChoicePromptOptionTypeNormal  ChoicePromptOptionType = "normal"
)

type prompt struct {
	UUID    string      `json:"id"`
	Type    PromptType  `json:"type"`
	Message string      `json:"message"`
	Payload interface{} `json:"payload,omitempty"`
	ch      chan io.Reader
}

type yesNoPromptResponse struct {
	Payload bool `json:"payload"`
}

type choicePromptResponse struct {
	Payload *ChoicePromptOption `json:"payload"`
}

func (p *prompt) ID() string {
	return p.UUID
}

func (p *prompt) Respond(res io.Reader) {
	p.ch <- res
}

func (ec *eventContext) SendPrompt(p *prompt) io.Reader {
	p.UUID = random.UUID()
	p.ch = make(chan io.Reader)
	ec.SendEvent(&Event{
		Type:    EventTypePrompt,
		Payload: p,
	})
	return <-p.ch
}

func (ec *eventContext) YesNoPrompt(msg string) bool {
	res := ec.SendPrompt(&prompt{
		Type:    PromptTypeYesNo,
		Message: msg,
	})
	data := &yesNoPromptResponse{}
	if err := json.NewDecoder(res).Decode(&data); err != nil {
		// TODO: send error log event
		return false
	}
	return data.Payload
}

func (ec *eventContext) ChoicePrompt(msg string, opts []ChoicePromptOption) *ChoicePromptOption {
	res := ec.SendPrompt(&prompt{
		Type:    PromptTypeChoice,
		Message: msg,
		Payload: &ChoicePrompt{
			Options: opts,
		},
	})
	data := &choicePromptResponse{}
	if err := json.NewDecoder(res).Decode(&data); err != nil {
		// TODO: send error log event
		return nil
	}
	return data.Payload
}
