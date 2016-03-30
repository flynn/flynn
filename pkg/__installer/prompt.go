package installer

import "io"

type PromptType string

var (
	PromptTypeYesNo          PromptType = "yes_no"
	PromptTypeChoice         PromptType = "choice"
	PromptTypeCredential     PromptType = "credential"
	PromptTypeInput          PromptType = "input"
	PromptTypeProtectedInput PromptType = "protected_input"
	PromptTypeFile           PromptType = "file"
)

type Prompt struct {
	Type    PromptType
	Message string
	Payload interface{}
}

type ChoicePrompt struct {
	Options []ChoicePromptOption
}

type ChoicePromptOption struct {
	Type  int
	Name  string
	Value string
}

type PromptResponse interface {
	Bool() bool
	Choice() *ChoicePromptOption
	Credential() Credential
	String() string
	File() (int, io.Reader, chan<- error)
}

func (c *Client) YesNoPrompt(msg string) (bool, error) {
	res, err := c.conf.Prompt(&Prompt{
		Type:    PromptTypeYesNo,
		Message: msg,
	})
	if err != nil {
		return false, err
	}
	return res.Bool(), nil
}

func (c *Client) ChoicePrompt(msg string, options []ChoicePromptOption) (*ChoicePromptOption, error) {
	res, err := c.conf.Prompt(&Prompt{
		Type:    PromptTypeChoice,
		Message: msg,
		Payload: ChoicePrompt{options},
	})
	if err != nil {
		return nil, err
	}
	return res.Choice(), nil
}

func (c *Client) CredentialPrompt(msg string) (Credential, error) {
	res, err := c.conf.Prompt(&Prompt{
		Type:    PromptTypeCredential,
		Message: msg,
	})
	if err != nil {
		return nil, err
	}
	return res.Credential(), nil
}

func (c *Client) InputPrompt(msg string) (string, error) {
	res, err := c.conf.Prompt(&Prompt{
		Type:    PromptTypeInput,
		Message: msg,
	})
	if err != nil {
		return "", err
	}
	return res.String(), nil
}

func (c *Client) ProtectedInputPrompt(msg string) (string, error) {
	res, err := c.conf.Prompt(&Prompt{
		Type:    PromptTypeProtectedInput,
		Message: msg,
	})
	if err != nil {
		return "", err
	}
	return res.String(), nil
}

func (c *Client) FilePrompt(msg string) (int, io.Reader, chan<- error, error) {
	res, err := c.conf.Prompt(&Prompt{
		Type:    PromptTypeFile,
		Message: msg,
	})
	if err != nil {
		return 0, nil, nil, err
	}
	size, r, readErrChan := res.File()
	return size, r, readErrChan, nil
}
