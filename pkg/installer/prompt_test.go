package installer

import (
	"net/http/httptest"
	"testing"

	. "github.com/flynn/go-check"
)

// Hook gocheck up to the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type S struct {
	srv *httptest.Server
}

var _ = Suite(&S{})

func (s *S) SetUpSuite(c *C) {
}

func respondToPromptEvent(c *C, ec *eventContext, res interface{}) {
	event := <-ec.ch
	c.Assert(event.Type, Equals, EventTypePrompt)
	p, ok := event.Payload.(*prompt)
	c.Assert(ok, Equals, true)
	p.Respond(res)
}

func (s *S) TestYesNoPrompt(c *C) {
	ec := &eventContext{
		ch: make(chan *Event),
	}

	go respondToPromptEvent(c, ec, YesNoPromptResponse{true})
	c.Assert(ec.YesNoPrompt("Yes or No?"), Equals, true)

	go respondToPromptEvent(c, ec, YesNoPromptResponse{false})
	c.Assert(ec.YesNoPrompt("Yes or No?"), Equals, false)
}

func (s *S) TestChoicePrompt(c *C) {
	ec := &eventContext{
		ch: make(chan *Event),
	}

	option := ChoicePromptOption{
		Description: "foo",
		Value:       "bar",
	}
	go respondToPromptEvent(c, ec, ChoicePromptResponse{&option})
	c.Assert(ec.ChoicePrompt("Pick one", []ChoicePromptOption{option}), DeepEquals, &option)
}
