package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"sync"
	"time"

	acme "github.com/eggsampler/acme/v3"
	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	discoverd "github.com/flynn/flynn/discoverd/client"
	"github.com/flynn/flynn/pkg/attempt"
	"github.com/flynn/flynn/pkg/stream"
	router "github.com/flynn/flynn/router/types"
	"github.com/inconshreveable/log15"
)

// DefaultDirectoryURL is the default ACME directory URL
const DefaultDirectoryURL = acme.LetsEncryptStaging

// ControllerClient is an interface that provides streaming and updating of managed
// certificates, and the creation and deletion of routes
type ControllerClient interface {
	StreamManagedCertificates(certs chan *ct.ManagedCertificate) (stream.Stream, error)

	UpdateManagedCertificate(cert *ct.ManagedCertificate) error

	CreateRoute(appID string, route *router.Route) error

	DeleteRoute(appID string, routeID string) error
}

// ACME manages ACME accounts and starts services for handling certificate
// issuance
type ACME struct {
	client *acme.Client
	log    log15.Logger
}

// New returns an ACME object that uses the given directoryURL, or
// DefaultDirectoryURL if it's empty
func New(directoryURL string, log log15.Logger, opts ...acme.OptionFunc) (*ACME, error) {
	if directoryURL == "" {
		directoryURL = DefaultDirectoryURL
	}
	client, err := acme.NewClient(directoryURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("error initializing ACME client: %s", err)
	}
	return &ACME{
		client: &client,
		log:    log,
	}, nil
}

// ACMEAccount returns the given account's existing acme.Account
func (a *ACME) ACMEAccount(account *Account) (acme.Account, error) {
	privKey, err := account.PrivateKey()
	if err != nil {
		return acme.Account{}, err
	}
	return a.client.NewAccount(privKey, true, account.TermsOfServiceAgreed, account.Contacts...)
}

// CheckExistingAccount checks that the given ACME account exists
func (a *ACME) CheckExistingAccount(account *Account) error {
	_, err := a.ACMEAccount(account)
	if err != nil {
		return fmt.Errorf("error getting existing ACME account: %s", err)
	}
	return nil
}

// CreateAccount creates the given ACME account
func (a *ACME) CreateAccount(account *Account) error {
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("error generating ACME account key: %s", err)
	}
	if _, err := a.client.NewAccount(privKey, false, account.TermsOfServiceAgreed, account.Contacts...); err != nil {
		return fmt.Errorf("error creating ACME account: %s", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("error encoding private key: %s", err)
	}
	account.Key = string(pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	}))
	return nil
}

// Service orders certificates for pending managed certficates using the ACME
// protocol
type Service struct {
	client      *acme.Client
	account     acme.Account
	controller  ControllerClient
	responder   *Responder
	handling    map[string]struct{}
	handlingMtx sync.Mutex
	stop        chan struct{}
	done        chan struct{}
	log         log15.Logger
}

// NewService returns a Service that uses the given account, controller client
// and responder
func (a *ACME) NewService(account *Account, controllerClient ControllerClient, responder *Responder) (*Service, error) {
	log := a.log.New("account", account.KeyID())

	log.Info("initializing ACME service")
	acmeAccount, err := a.ACMEAccount(account)
	if err != nil {
		log.Error("error initializing ACME service", "err", err)
		return nil, err
	}
	return &Service{
		client:     a.client,
		account:    acmeAccount,
		controller: controllerClient,
		responder:  responder,
		handling:   make(map[string]struct{}),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		log:        log,
	}, nil
}

// RunService runs an ACME service with configuration from environment variables
func RunService(ctx context.Context) error {
	log := log15.New("component", "acme")

	log.Info("getting ACME account from environment variables")
	account, err := NewAccountFromEnv()
	if err != nil {
		log.Error("error getting ACME account from environment variables", "err", err)
		return err
	}

	log.Info("initializing controller client")
	instances, err := discoverd.NewService("controller").Instances()
	if err != nil {
		log.Error("error initializing controller client", "err", err)
		return err
	}
	inst := instances[0]
	client, err := controller.NewClient("http://"+inst.Addr, inst.Meta["AUTH_KEY"])
	if err != nil {
		log.Error("error initializing controller client", "err", err)
		return err
	}

	log.Info("initializing responder")
	responderPort := os.Getenv("PORT")
	if responderPort == "" {
		responderPort = defaultResponderPort
	}
	responder, err := NewResponder(client, discoverd.DefaultClient, ":"+responderPort, log)
	if err != nil {
		log.Error("error initializing responder", "err", err)
		return err
	}

	directoryURL := os.Getenv("DIRECTORY_URL")
	if directoryURL != "" {
		directoryURL = DefaultDirectoryURL
	}
	log.Info("running ACME service", "directory_url", directoryURL)
	acme, err := New(directoryURL, log)
	if err != nil {
		log.Error("error running ACME service", "err", err)
		return err
	}
	service, err := acme.NewService(account, client, responder)
	if err != nil {
		log.Error("error running ACME service", "err", err)
		return err
	}
	return service.Run(ctx)
}

// Run runs the Service until either it stops or the given context is cancelled
func (s *Service) Run(ctx context.Context) error {
	if err := s.Start(); err != nil {
		return err
	}
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		s.Stop()
		return ctx.Err()
	}
}

// Start starts the Service watching and handling managed certificates
func (s *Service) Start() error {
	// define a connect function so we can handle re-connecting if the
	// stream drops
	var certs chan *ct.ManagedCertificate
	var stream stream.Stream
	connect := func() (err error) {
		s.log.Info("streaming managed certificates")
		certs = make(chan *ct.ManagedCertificate)
		stream, err = s.controller.StreamManagedCertificates(certs)
		if err != nil {
			s.log.Error("error streaming managed certificates", "err", err)
		}
		return
	}

	// try connecting immediately
	if err := connect(); err != nil {
		return err
	}

	// process events in a goroutine, re-connecting if necessary
	go func() {
		defer close(s.done)
		defer stream.Close()
		for {
			s.log.Info("processing managed certificate events")
		eventLoop:
			for {
				select {
				case cert, ok := <-certs:
					if !ok {
						s.log.Error("error streaming managed certificates", "err", stream.Err())
						break eventLoop
					}
					if cert.Status != ct.ManagedCertificateStatusPending {
						continue
					}
					s.handlingMtx.Lock()
					if _, ok := s.handling[cert.Domain]; !ok {
						s.log.Info("handling pending managed certificate", "domain", cert.Domain)
						go s.Handle(cert)
						s.handling[cert.Domain] = struct{}{}
					}
					s.handlingMtx.Unlock()
				case <-s.stop:
					return
				}
			}
			s.log.Info("reconnecting managed certificate stream")
			for {
				select {
				case <-s.stop:
					return
				default:
				}
				if err := connect(); err == nil {
					break
				}
				time.Sleep(time.Second)
			}
		}
	}()
	return nil
}

// Stop stops the Service from processing managed certificates
func (s *Service) Stop() {
	close(s.stop)
	<-s.done
}

// Handle handles the given managed certificate by placing an ACME order,
// satisfying authz challenges, fetching issued certificates and passing them
// to the controller to update the associated routes
func (s *Service) Handle(cert *ct.ManagedCertificate) {
	// cleanup s.handling once done
	defer func() {
		s.handlingMtx.Lock()
		delete(s.handling, cert.Domain)
		s.handlingMtx.Unlock()
	}()

	// make sure we have an order
	order, err := s.getOrCreateOrder(cert)
	if err != nil {
		s.setFailed(cert, "error creating order: %s", err)
		return
	}

	// make sure the order has a http-01 challenge
	challenge := s.getHTTP01Challenge(cert, order)
	if challenge == nil {
		s.setFailed(cert, "order does not have a http-01 challenge: %s", order.URL)
		return
	}

	// satisfy the http-01 challenge
	if err := s.satisfyHTTP01Challenge(cert, challenge); err != nil {
		s.setFailed(cert, "error satisfying http-01 challenge: %s", err)
		return
	}

	// finalize the order
	order, keyDER, err := s.finalizeOrder(cert, order)
	if err != nil {
		s.setFailed(cert, "error finalizing order: %s", err)
		return
	}

	// set the current certificate to the newly issued certificate
	if err := s.setCertificate(cert, keyDER, order); err != nil {
		s.setFailed(cert, "error setting current certificate: %s", err)
		return
	}

	return
}

func (s *Service) getOrCreateOrder(cert *ct.ManagedCertificate) (*acme.Order, error) {
	log := s.log.New("domain", cert.Domain)

	// if the managed certificate already has an OrderURL, fetch it and
	// check if it's valid
	if url := cert.OrderURL; url != "" {
		log.Info("checking existing order", "order.url", url)
		order, err := s.client.FetchOrder(s.account, url)
		if err != nil {
			log.Error("error checking existing order", "order.url", url, "err", err)
		} else if order.Status == "invalid" {
			log.Error("existing order is invalid", "order.url", url)
		} else {
			return &order, nil
		}
	}

	// the managed certificate either has no OrderURL, or the order the
	// OrderURL points to is not valid, so create a new order
	log.Info("creating new order")
	ids := []acme.Identifier{{Type: "dns", Value: cert.Domain}}
	order, err := s.client.NewOrder(s.account, ids)
	if err != nil {
		log.Error("error creating new order", "err", err)
		return nil, err
	}

	log.Info("updating managed certificate with new order", "order.url", order.URL)
	cert.OrderURL = order.URL
	if err := s.controller.UpdateManagedCertificate(cert); err != nil {
		log.Error("error updating managed certificate with new order", "order.url", order.URL, "err", err)
		return nil, err
	}

	return &order, nil
}

func (s *Service) getHTTP01Challenge(cert *ct.ManagedCertificate, order *acme.Order) *acme.Challenge {
	log := s.log.New("domain", cert.Domain)

	log.Info("getting http-01 challenge")
	for _, authURL := range order.Authorizations {
		auth, err := s.client.FetchAuthorization(s.account, authURL)
		if err != nil {
			continue
		}
		if challenge, ok := auth.ChallengeMap[acme.ChallengeTypeHTTP01]; ok {
			log.Info("using http-01 challenge", "challenge.token", challenge.Token, "challenge.url", challenge.URL)
			return &challenge
		}
	}
	log.Error("missing http-01 challenge")
	return nil
}

func (s *Service) satisfyHTTP01Challenge(cert *ct.ManagedCertificate, challenge *acme.Challenge) error {
	log := s.log.New("domain", cert.Domain, "challenge.token", challenge.Token)

	log.Info("configuring http-01 challenge response")
	return s.responder.RespondHTTP01(cert, challenge, func() error {
		// make multiple attempts to complete the challenge
		attempts := attempt.Strategy{
			Total: 60 * time.Second,
			Delay: 1 * time.Second,
		}
		return attempts.Run(func() error {
			// TODO: check the challenge is still valid

			// complete the challenge
			log.Info("completing http-01 challenge")
			if _, err := s.client.UpdateChallenge(s.account, *challenge); err != nil {
				log.Error("error completing http-01 challenge", "err", err)
				return err
			}

			return nil
		})
	})
}

func (s *Service) finalizeOrder(cert *ct.ManagedCertificate, order *acme.Order) (*acme.Order, []byte, error) {
	log := s.log.New("domain", cert.Domain)

	log.Info("generating a CSR")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Error("error generating private key", "err", err)
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		log.Error("error encoding private key", "err", err)
		return nil, nil, err
	}
	csrTmpl := &x509.CertificateRequest{
		SignatureAlgorithm: x509.ECDSAWithSHA256,
		PublicKeyAlgorithm: x509.ECDSA,
		PublicKey:          key.Public(),
		Subject:            pkix.Name{CommonName: cert.Domain},
		DNSNames:           []string{cert.Domain},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTmpl, key)
	if err != nil {
		log.Error("error generating CSR", "err", err)
		return nil, nil, err
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		log.Error("error generating CSR", "err", err)
		return nil, nil, err
	}

	log.Info("finalizing order")
	finalizedOrder, err := s.client.FinalizeOrder(s.account, *order, csr)
	if err != nil {
		log.Error("error finalizing order", "err", err)
		return nil, nil, err
	}

	return &finalizedOrder, keyDER, nil
}

func (s *Service) setCertificate(cert *ct.ManagedCertificate, keyDER []byte, order *acme.Order) error {
	log := s.log.New("domain", cert.Domain)

	log.Info("fetching issued certificate")
	issuedCerts, err := s.client.FetchCertificates(s.account, order.Certificate)
	if err != nil {
		log.Error("error fetching issued certificate", "err", err)
		return err
	}
	cert.Status = ct.ManagedCertificateStatusIssued
	cert.Certificate = &router.Certificate{
		Chain: make([][]byte, len(issuedCerts)),
		Key:   keyDER,
	}
	for i, issuedCert := range issuedCerts {
		cert.Certificate.Chain[i] = issuedCert.Raw
	}

	log.Info("setting current certificate", "certificate.id", cert.Certificate.ID().String(), "key.id", cert.Certificate.KeyID().String())
	if err := s.controller.UpdateManagedCertificate(cert); err != nil {
		log.Error("error setting current certificate", "err", err)
		return err
	}

	return nil
}

func (s *Service) setFailed(cert *ct.ManagedCertificate, format string, v ...interface{}) {
	detail := fmt.Sprintf(format, v...)
	s.log.Info("setting certificate status to failed", "domain", cert.Domain, "detail", detail)
	cert.Status = ct.ManagedCertificateStatusFailed
	cert.AddError("", detail)
	s.controller.UpdateManagedCertificate(cert)
}
