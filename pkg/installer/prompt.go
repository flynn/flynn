package installer

import (
	"fmt"

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
	PromptTypeSSHKeys        PromptType = "ssh_keys"
)

type ChoicePrompt struct {
	Options []ChoicePromptOption `json:"options"`
}

type ChoicePromptOption struct {
	Type        ChoicePromptOptionType `json:"type"`
	Description string                 `json:"description"`
	Value       string                 `json:"value"`
}

type ChoicePromptOptionType string

var (
	ChoicePromptOptionTypePrimary ChoicePromptOptionType = "primary"
	ChoicePromptOptionTypeNormal  ChoicePromptOptionType = "normal"
)

// YesNoPromptResponse is the response for a YesNo prompt
type YesNoPromptResponse struct {
	Response bool `json:"payload"`
}

// ChoicePromptResponse is the response for a Choice prompt
type ChoicePromptResponse struct {
	Response *ChoicePromptOption `json:"payload"`
}

type SSHKeysPromptResponse struct {
	Response []*SSHKey `json:"ssh_keys"`
}

type prompt struct {
	UUID       string      `json:"id"`
	PromptType PromptType  `json:"type"`
	Message    string      `json:"message"`
	Payload    interface{} `json:"payload,omitempty"`
	ch         chan interface{}
}

func (p *prompt) ID() string {
	return p.UUID
}

func (p *prompt) Type() PromptType {
	return p.PromptType
}

func (p *prompt) Respond(res interface{}) {
	p.ch <- res
}

func (p *prompt) ResponseExample() interface{} {
	switch p.PromptType {
	case PromptTypeYesNo:
		return YesNoPromptResponse{}
	case PromptTypeChoice:
		return ChoicePromptResponse{}
	case PromptTypeSSHKeys:
		return SSHKeysPromptResponse{}
	default:
		return nil
	}
}

func (ec *eventContext) invalidPromptResponse(p Prompt, res interface{}) {
	ec.Log(LogLevelDebug, fmt.Sprintf("Invalid response given for prompt(%s), expected %T but got %T", p.ID(), p.ResponseExample(), res))
}

func (ec *eventContext) SendPrompt(p *prompt) interface{} {
	p.UUID = random.UUID()
	p.ch = make(chan interface{})
	ec.SendEvent(&Event{
		Type:    EventTypePrompt,
		Payload: p,
	})
	return <-p.ch
}

func (ec *eventContext) YesNoPrompt(msg string) bool {
	p := &prompt{
		PromptType: PromptTypeYesNo,
		Message:    msg,
	}
	res := ec.SendPrompt(p)
	if r, ok := res.(YesNoPromptResponse); ok {
		return r.Response
	}
	ec.invalidPromptResponse(p, res)
	return false
}

func (ec *eventContext) ChoicePrompt(msg string, opts []ChoicePromptOption) *ChoicePromptOption {
	p := &prompt{
		PromptType: PromptTypeChoice,
		Message:    msg,
		Payload: &ChoicePrompt{
			Options: opts,
		},
	}
	res := ec.SendPrompt(p)
	if r, ok := res.(ChoicePromptResponse); ok {
		return r.Response
	}
	ec.invalidPromptResponse(p, res)
	return nil
}

func (ec *eventContext) SSHKeysPrompt() []*SSHKey {
	p := &prompt{
		PromptType: PromptTypeSSHKeys,
	}
	res := ec.SendPrompt(p)
	if r, ok := res.(SSHKeysPromptResponse); ok {
		return r.Response
	}
	ec.invalidPromptResponse(p, res)
	return nil
}
