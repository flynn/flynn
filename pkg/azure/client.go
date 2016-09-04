package azure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/oauth2"
)

const (
	jsonAPIVersion  = "2015-01-01"
	JSONAPIResource = "https://management.azure.com/"
	XMLAPIResource  = "https://management.core.windows.net/"
)

func OAuth2Config(clientID, tokenExchangeURL, resource string) *oauth2.Config {
	return &oauth2.Config{
		ClientID: clientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: tokenExchangeURL,
		},
		Params: url.Values{
			"resource": {resource},
		},
	}
}

func NewClient(jsonClient, xmlClient *http.Client) *Client {
	return &Client{
		json: jsonClient,
		xml:  xmlClient,
	}
}

type Client struct {
	json *http.Client
	xml  *http.Client
}

type locationsResponse struct {
	Namespace     string `json:"namespace"`
	ResourceTypes []struct {
		Type      string   `json:"resourceType"`
		Locations []string `json:"locations"`
	} `json:"resourceTypes"`
}

func (c *Client) ListLocations(providerNamespace, resourceType string) ([]string, error) {
	res, err := c.doJSONRequest("GET", fmt.Sprintf("/providers/%s", providerNamespace), nil, jsonAPIVersion)
	if err != nil {
		return nil, err
	}
	var locationsRes *locationsResponse
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&locationsRes); err != nil {
		return nil, err
	}
	var locations []string
	for _, t := range locationsRes.ResourceTypes {
		if t.Type != resourceType {
			continue
		}
		locations = t.Locations
		break
	}
	return locations, nil
}

type jsonErrorResponse struct {
	Error jsonErrorResponseInner `json:"error"`
}

type jsonErrorResponseInner struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) doJSONRequest(method, path string, body interface{}, apiVersion string) (*http.Response, error) {
	var encodedBody bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encodedBody).Encode(body); err != nil {
			return nil, err
		}
	}
	uri, err := url.Parse(JSONAPIResource)
	if err != nil {
		return nil, err
	}
	pathURI, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	uri = uri.ResolveReference(pathURI)
	query := uri.Query()
	query.Add("api-version", apiVersion)
	uri.RawQuery = query.Encode()
	var req *http.Request
	if body == nil {
		req, err = http.NewRequest(method, uri.String(), nil)
	} else {
		req, err = http.NewRequest(method, uri.String(), &encodedBody)
	}
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/json")
	if body != nil {
		req.Header.Add("Content-Type", "application/json")
	}
	res, err := c.json.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		var errRes jsonErrorResponse
		dec := json.NewDecoder(res.Body)
		if err := dec.Decode(&errRes); err != nil {
			return nil, err
		}
		return nil, &httphelper.JSONError{
			Code:    httphelper.UnknownErrorCode,
			Message: errRes.Error.Message,
		}
	}
	return res, nil
}

type jsonResponse struct {
	Properties json.RawMessage `json:"properties"`
}

func (c *Client) Get(path string, properties interface{}) error {
	res, err := c.doJSONRequest("GET", path, nil, "2015-05-01-preview")
	if err != nil {
		return err
	}
	var data jsonResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return err
	}
	if err := json.Unmarshal(data.Properties, properties); err != nil {
		return err
	}
	return nil
}

type Subscription struct {
	ID     string `xml:"SubscriptionID" json:"id"`
	Name   string `xml:"SubscriptionName" json:"name"`
	Status string `xml:"SubscriptionStatus" json:"status"`
}

type subscriptionsResponse struct {
	Subs []struct {
		ID     string `json:"subscriptionId"`
		Name   string `json:"displayName"`
		Status string `json:"state"`
	} `json:"value"`
}

func (c *Client) ListSubscriptions() ([]Subscription, error) {
	// The /subscriptions endpoint requires a token generated for the xml api
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/subscriptions?api-version=%s", JSONAPIResource, jsonAPIVersion), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.xml.Do(req)
	if err != nil {
		return nil, err
	}
	var sres subscriptionsResponse
	dec := json.NewDecoder(res.Body)
	if err := dec.Decode(&sres); err != nil {
		return nil, err
	}
	subscriptions := make([]Subscription, len(sres.Subs))
	for i, s := range sres.Subs {
		subscriptions[i] = Subscription{
			ID:     s.ID,
			Name:   s.Name,
			Status: s.Status,
		}
	}
	return subscriptions, nil
}

type ResourceGroup struct {
	SubscriptionID string            `json:"-"`
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name,omitempty"`
	Location       string            `json:"location"`
	Tags           map[string]string `json:"tags,omitempty"`
	Properties     map[string]string `json:"properties,omitempty"`
}

func (c *Client) CreateResourceGroup(rg *ResourceGroup) (*ResourceGroup, error) {
	subscriptionID := rg.SubscriptionID
	name := rg.Name
	rg = &ResourceGroup{
		Location: rg.Location,
		Tags:     rg.Tags,
	}
	res, err := c.doJSONRequest("PUT", fmt.Sprintf("/subscriptions/%s/resourcegroups/%s", subscriptionID, name), rg, jsonAPIVersion)
	if err != nil {
		return nil, err
	}
	if err := json.NewDecoder(res.Body).Decode(&rg); err != nil {
		return nil, err
	}
	return rg, nil
}

func (c *Client) DeleteResourceGroup(subscriptionID, resourceGroupName string) error {
	res, err := c.doJSONRequest("DELETE", fmt.Sprintf("/subscriptions/%s/resourcegroups/%s", subscriptionID, resourceGroupName), nil, jsonAPIVersion)
	if err != nil {
		return err
	}
	if res.StatusCode != 202 {
		fmt.Println(res.StatusCode)
		return nil
	}
	statusURL := res.Header.Get("Location")
	interval := 5 * time.Second
	for {
		res, err := c.doJSONRequest("GET", statusURL, nil, jsonAPIVersion)
		if err != nil {
			return err
		}
		if res.StatusCode != 202 {
			return nil
		}
		time.Sleep(interval)
		// TODO(jvatic): This should eventually timeout
	}
}

type templateDeploymentRequestWrapper struct {
	Properties *TemplateDeploymentRequest `json:"properties"`
}

type TemplateParam struct {
	Value json.RawMessage `json:"value"`
}

type TemplateDeploymentRequest struct {
	SubscriptionID    string                    `json:"-"`
	ResourceGroupName string                    `json:"-"`
	Name              string                    `json:"-"`
	Parameters        map[string]*TemplateParam `json:"parameters"`
	Template          json.RawMessage           `json:"template"`
	Mode              string                    `json:"mode"`
}

type templateDeploymentResponseWrapper struct {
	ID         string                      `json:"id"`
	Name       string                      `json:"name"`
	Properties *TemplateDeploymentResponse `json:"properties"`
}

type TemplateDeploymentResponse struct {
	ID                string
	Name              string
	Parameters        map[string]*json.RawMessage `json:"parameters"`
	Mode              string                      `json:"mode"`
	ProvisioningState string                      `json:"provisioningState"`
	Timestamp         *time.Time                  `json:"timestamp"`
	CorrelationID     string                      `json:"correlationId"`
	Outputs           json.RawMessage             `json:"outputs"`
	Providers         json.RawMessage             `json:"providers"`
	Dependencies      json.RawMessage             `json:"dependencies"`
}

func MustTemplateParam(v interface{}) *TemplateParam {
	var raw json.RawMessage
	var err error
	raw, err = json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return &TemplateParam{Value: raw}
}

func (c *Client) CreateTemplateDeployment(tdr *TemplateDeploymentRequest) (*TemplateDeploymentResponse, error) {
	tdr.Mode = "Incremental"
	res, err := c.doJSONRequest("PUT", fmt.Sprintf("/subscriptions/%s/resourcegroups/%s/providers/microsoft.resources/deployments/%s", tdr.SubscriptionID, tdr.ResourceGroupName, tdr.Name), &templateDeploymentRequestWrapper{tdr}, jsonAPIVersion)
	if err != nil {
		return nil, err
	}
	var trw templateDeploymentResponseWrapper
	if err := json.NewDecoder(res.Body).Decode(&trw); err != nil {
		return nil, err
	}
	templateResponse := trw.Properties
	templateResponse.ID = trw.ID
	templateResponse.Name = trw.Name
	return templateResponse, nil
}

func (c *Client) WaitForTemplateDeployment(subscriptionID, resourceGroupName, deploymentName string, outputs interface{}) error {
	for {
		res, err := c.doJSONRequest("GET", fmt.Sprintf("/subscriptions/%s/resourcegroups/%s/providers/microsoft.resources/deployments/%s", subscriptionID, resourceGroupName, deploymentName), nil, jsonAPIVersion)
		if err != nil {
			return err
		}
		var rawBody bytes.Buffer
		if _, err := io.Copy(&rawBody, res.Body); err != nil {
			return err
		}
		var trw templateDeploymentResponseWrapper
		if err := json.NewDecoder(bytes.NewReader(rawBody.Bytes())).Decode(&trw); err != nil {
			return err
		}
		switch trw.Properties.ProvisioningState {
		case "Accepted":
		case "Ready":
		case "Running":
		case "Canceled":
			return errors.New("Template deployment canceled")
		case "Failed":
			return errors.New("Template deployment failed")
		case "Deleted":
			return errors.New("Template deployment deleted")
		case "Succeeded":
			if err := json.Unmarshal(trw.Properties.Outputs, outputs); err != nil {
				return err
			}
			return nil
		}
		time.Sleep(5 * time.Second)
		// TODO(jvatic): This should eventually timeout
	}
}
