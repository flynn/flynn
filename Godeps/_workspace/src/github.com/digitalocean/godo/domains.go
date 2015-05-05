package godo

import "fmt"

const domainsBasePath = "v2/domains"

// DomainsService is an interface for managing DNS with the Digital Ocean API.
// See: https://developers.digitalocean.com/documentation/v2#domains and
// https://developers.digitalocean.com/documentation/v2#domain-records
type DomainsService interface {
	List(*ListOptions) ([]Domain, *Response, error)
	Get(string) (*DomainRoot, *Response, error)
	Create(*DomainCreateRequest) (*DomainRoot, *Response, error)
	Delete(string) (*Response, error)

	Records(string, *ListOptions) ([]DomainRecord, *Response, error)
	Record(string, int) (*DomainRecord, *Response, error)
	DeleteRecord(string, int) (*Response, error)
	EditRecord(string, int, *DomainRecordEditRequest) (*DomainRecord, *Response, error)
	CreateRecord(string, *DomainRecordEditRequest) (*DomainRecord, *Response, error)
}

// DomainsServiceOp handles communication with the domain related methods of the
// DigitalOcean API.
type DomainsServiceOp struct {
	client *Client
}

var _ DomainsService = &DomainsServiceOp{}

// Domain represents a Digital Ocean domain
type Domain struct {
	Name     string `json:"name"`
	TTL      int    `json:"ttl"`
	ZoneFile string `json:"zone_file"`
}

// DomainRoot represents a response from the Digital Ocean API
type DomainRoot struct {
	Domain *Domain `json:"domain"`
}

type domainsRoot struct {
	Domains []Domain `json:"domains"`
	Links   *Links   `json:"links"`
}

// DomainCreateRequest respresents a request to create a domain.
type DomainCreateRequest struct {
	Name      string `json:"name"`
	IPAddress string `json:"ip_address"`
}

// DomainRecordRoot is the root of an individual Domain Record response
type DomainRecordRoot struct {
	DomainRecord *DomainRecord `json:"domain_record"`
}

// DomainRecordsRoot is the root of a group of Domain Record responses
type DomainRecordsRoot struct {
	DomainRecords []DomainRecord `json:"domain_records"`
	Links         *Links         `json:"links"`
}

// DomainRecord represents a DigitalOcean DomainRecord
type DomainRecord struct {
	ID       int    `json:"id,float64,omitempty"`
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
	Data     string `json:"data,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Port     int    `json:"port,omitempty"`
	Weight   int    `json:"weight,omitempty"`
}

// DomainRecordEditRequest represents a request to update a domain record.
type DomainRecordEditRequest struct {
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
	Data     string `json:"data,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Port     int    `json:"port,omitempty"`
	Weight   int    `json:"weight,omitempty"`
}

func (d Domain) String() string {
	return Stringify(d)
}

// List all domains
func (s DomainsServiceOp) List(opt *ListOptions) ([]Domain, *Response, error) {
	path := domainsBasePath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(domainsRoot)
	resp, err := s.client.Do(req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}

	return root.Domains, resp, err
}

// Get individual domain
func (s *DomainsServiceOp) Get(name string) (*DomainRoot, *Response, error) {
	path := fmt.Sprintf("%s/%s", domainsBasePath, name)

	req, err := s.client.NewRequest("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(DomainRoot)
	resp, err := s.client.Do(req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, err
}

// Create a new domain
func (s *DomainsServiceOp) Create(createRequest *DomainCreateRequest) (*DomainRoot, *Response, error) {
	path := domainsBasePath

	req, err := s.client.NewRequest("POST", path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(DomainRoot)
	resp, err := s.client.Do(req, root)
	if err != nil {
		return nil, resp, err
	}

	return root, resp, err
}

// Delete droplet
func (s *DomainsServiceOp) Delete(name string) (*Response, error) {
	path := fmt.Sprintf("%s/%s", domainsBasePath, name)

	req, err := s.client.NewRequest("DELETE", path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req, nil)

	return resp, err
}

// Converts a DomainRecord to a string.
func (d DomainRecord) String() string {
	return Stringify(d)
}

// Converts a DomainRecordEditRequest to a string.
func (d DomainRecordEditRequest) String() string {
	return Stringify(d)
}

// Records returns a slice of DomainRecords for a domain
func (s *DomainsServiceOp) Records(domain string, opt *ListOptions) ([]DomainRecord, *Response, error) {
	path := fmt.Sprintf("%s/%s/records", domainsBasePath, domain)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(DomainRecordsRoot)
	resp, err := s.client.Do(req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}

	return root.DomainRecords, resp, err
}

// Record returns the record id from a domain
func (s *DomainsServiceOp) Record(domain string, id int) (*DomainRecord, *Response, error) {
	path := fmt.Sprintf("%s/%s/records/%d", domainsBasePath, domain, id)

	req, err := s.client.NewRequest("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}

	record := new(DomainRecordRoot)
	resp, err := s.client.Do(req, record)
	if err != nil {
		return nil, resp, err
	}

	return record.DomainRecord, resp, err
}

// DeleteRecord deletes a record from a domain identified by id
func (s *DomainsServiceOp) DeleteRecord(domain string, id int) (*Response, error) {
	path := fmt.Sprintf("%s/%s/records/%d", domainsBasePath, domain, id)

	req, err := s.client.NewRequest("DELETE", path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req, nil)

	return resp, err
}

// EditRecord edits a record using a DomainRecordEditRequest
func (s *DomainsServiceOp) EditRecord(
	domain string,
	id int,
	editRequest *DomainRecordEditRequest) (*DomainRecord, *Response, error) {
	path := fmt.Sprintf("%s/%s/records/%d", domainsBasePath, domain, id)

	req, err := s.client.NewRequest("PUT", path, editRequest)
	if err != nil {
		return nil, nil, err
	}

	d := new(DomainRecord)
	resp, err := s.client.Do(req, d)
	if err != nil {
		return nil, resp, err
	}

	return d, resp, err
}

// CreateRecord creates a record using a DomainRecordEditRequest
func (s *DomainsServiceOp) CreateRecord(
	domain string,
	createRequest *DomainRecordEditRequest) (*DomainRecord, *Response, error) {
	path := fmt.Sprintf("%s/%s/records", domainsBasePath, domain)
	req, err := s.client.NewRequest("POST", path, createRequest)

	if err != nil {
		return nil, nil, err
	}

	d := new(DomainRecordRoot)
	resp, err := s.client.Do(req, d)
	if err != nil {
		return nil, resp, err
	}

	return d.DomainRecord, resp, err
}
