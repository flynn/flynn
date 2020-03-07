package bootstrap

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/flynn/flynn/router/acme"
	"github.com/inconshreveable/log15"
)

type GenACMEAccountAction struct {
	ID string `json:"id"`

	ACMEConfig
}

func init() {
	Register("gen-acme-account", &GenACMEAccountAction{})
}

type ACMEConfig struct {
	DirectoryURL         string `json:"directory_url"`
	Key                  string `json:"key"`
	Contacts             string `json:"contacts"`
	TermsOfServiceAgreed string `json:"terms_of_service_agreed"`
}

func (a *GenACMEAccountAction) Run(s *State) error {
	data := a.ACMEConfig
	s.StepData[a.ID] = &data

	// interpolate the config
	data.DirectoryURL = interpolate(s, data.DirectoryURL)
	data.Key = interpolate(s, data.Key)
	data.Contacts = interpolate(s, data.Contacts)
	data.TermsOfServiceAgreed = interpolate(s, data.TermsOfServiceAgreed)

	// construct an ACME account
	account := &acme.Account{
		Key: data.Key,
	}
	if len(data.Contacts) > 0 {
		account.Contacts = strings.Split(data.Contacts, ",")
	}
	if v, err := strconv.ParseBool(data.TermsOfServiceAgreed); err == nil {
		account.TermsOfServiceAgreed = v
	}

	// initialize ACME
	acme, err := acme.New(data.DirectoryURL, log15.New())
	if err != nil {
		return err
	}

	// if the account already has a key, check that it exists
	if account.Key != "" {
		if err := acme.CheckExistingAccount(account); err != nil {
			return fmt.Errorf("error checking exiting ACME account: %s", err)
		}
		return nil
	}

	// create the account and add the key to the step data so it can be
	// referenced by other steps
	if err := acme.CreateAccount(account); err != nil {
		return fmt.Errorf("error creating ACME account: %s", err)
	}
	data.Key = account.Key

	return nil
}
