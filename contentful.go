package contentful

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"time"

	"moul.io/http2curl"
)

// Contentful model
type Contentful struct {
	client      *http.Client
	api         string
	token       string
	Debug       bool
	QueryParams map[string]string
	Headers     map[string]string
	BaseURL     string
	Environment string

	Spaces       *SpacesService
	APIKeys      *APIKeyService
	Assets       *AssetsService
	ContentTypes *ContentTypesService
	Entries      *EntriesService
	Locales      *LocalesService
	Webhooks     *WebhooksService
	ScheduledActions     *ScheduledActionsService
}

type service struct {
	c *Contentful
}

func getEnvPath(c *Contentful) (envPath string) {
	if c.Environment != "" {
		envPath = fmt.Sprintf("/environments/%s", c.Environment)
		return
	}
	return
}

// NewCMA returns a CMA client
func NewCMA(token string) *Contentful {
	c := &Contentful{
		client: http.DefaultClient,
		api:    "CMA",
		token:  token,
		Debug:  false,
		Headers: map[string]string{
			"Authorization":           fmt.Sprintf("Bearer %s", token),
			// "Content-Type":            "application/vnd.contentful.management.v1+json",
			// "X-Contentful-User-Agent": fmt.Sprintf("sdk contentful.go/%s", Version),
		},
		BaseURL: "https://api.contentful.com",
	}

	c.Spaces = &SpacesService{c: c}
	c.APIKeys = &APIKeyService{c: c}
	c.Assets = &AssetsService{c: c}
	c.ContentTypes = &ContentTypesService{c: c}
	c.Entries = &EntriesService{c: c}
	c.Locales = &LocalesService{c: c}
	c.Webhooks = &WebhooksService{c: c}
	c.ScheduledActions = &ScheduledActionsService{c: c}

	return c
}

// NewCDA returns a CDA client
func NewCDA(token string) *Contentful {
	c := &Contentful{
		client: http.DefaultClient,
		api:    "CDA",
		token:  token,
		Debug:  false,
		Headers: map[string]string{
			"Authorization":           "Bearer " + token,
			"Content-Type":            "application/vnd.contentful.delivery.v1+json",
			"X-Contentful-User-Agent": fmt.Sprintf("contentful-go/%s", Version),
		},
		BaseURL: "https://cdn.contentful.com",
	}

	c.Spaces = &SpacesService{c: c}
	c.APIKeys = &APIKeyService{c: c}
	c.Assets = &AssetsService{c: c}
	c.ContentTypes = &ContentTypesService{c: c}
	c.Entries = &EntriesService{c: c}
	c.Locales = &LocalesService{c: c}
	c.Webhooks = &WebhooksService{c: c}

	return c
}

// NewCPA returns a CPA client
func NewCPA(token string) *Contentful {
	c := &Contentful{
		client: http.DefaultClient,
		Debug:  false,
		api:    "CPA",
		token:  token,
		Headers: map[string]string{
			"Authorization": "Bearer " + token,
		},
		BaseURL: "https://preview.contentful.com",
	}

	c.Spaces = &SpacesService{c: c}
	c.APIKeys = &APIKeyService{c: c}
	c.Assets = &AssetsService{c: c}
	c.ContentTypes = &ContentTypesService{c: c}
	c.Entries = &EntriesService{c: c}
	c.Locales = &LocalesService{c: c}
	c.Webhooks = &WebhooksService{c: c}

	return c
}

// SetOrganization sets the given organization id
func (c *Contentful) SetOrganization(organizationID string) *Contentful {
	c.Headers["X-Contentful-Organization"] = organizationID

	return c
}

// SetHTTPClient sets the underlying http.Client used to make requests.
func (c *Contentful) SetHTTPClient(client *http.Client) *Contentful {
	c.client = client
	return c
}

// SetHTTPTransport creates a new http.Client and sets a custom Roundtripper.
func (c *Contentful) SetHTTPTransport(t http.RoundTripper) *Contentful {
	c.client = &http.Client{
		Transport: t,
	}
	return c
}

// SetBaseURL provides an option to change the BaseURL of the client
func (c *Contentful) SetBaseURL(url string) *Contentful {
	c.BaseURL = url
	return c
}

func (c *Contentful) newRequest(method, path string, query url.Values, body io.Reader) (*http.Request, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}

	// set query params
	for key, value := range c.QueryParams {
		query.Set(key, value)
	}

	u.Path = u.Path + path
	u.RawQuery = query.Encode()

	fmt.Printf("QUERY %s\n",u.RawQuery)
	fmt.Printf("X %s\n",u.String())

	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}

	// set headers
	for key, value := range c.Headers {
		req.Header.Set(key, value)
	}

	return req, nil
}

func (c *Contentful) do(req *http.Request, v interface{}) error {
	if c.Debug == true {
		command, _ := http2curl.GetCurlCommand(req)
		fmt.Println(command)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}

	if res.StatusCode >= 200 && res.StatusCode < 400 {
		if v != nil {
			defer res.Body.Close()
			err = json.NewDecoder(res.Body).Decode(v)
			if err != nil {
				return err
			}
		}

		return nil
	}

	// parse api response
	apiError := c.handleError(req, res)

	// return apiError if it is not rate limit error
	if _, ok := apiError.(RateLimitExceededError); !ok {
		return apiError
	}

	resetHeader := res.Header.Get("x-contentful-ratelimit-reset")

	// return apiError if Ratelimit-Reset header is not presented
	if resetHeader == "" {
		return apiError
	}

	// wait X-Contentful-Ratelimit-Reset amount of seconds
	waitSeconds, err := strconv.Atoi(resetHeader)
	if err != nil {
		return apiError
	}

	time.Sleep(time.Second * time.Duration(waitSeconds))

	return c.do(req, v)
}

func (c *Contentful) handleError(req *http.Request, res *http.Response) error {
	if c.Debug == true {
		dump, err := httputil.DumpResponse(res, true)
		if err != nil {
			log.Fatal(err)
		}

		fmt.Printf("%q", dump)
	}

	var e ErrorResponse
	defer res.Body.Close()
	err := json.NewDecoder(res.Body).Decode(&e)
	if err != nil {
		return err
	}

	apiError := APIError{
		req: req,
		res: res,
		err: &e,
	}

	switch errType := e.Sys.ID; errType {
	case "NotFound":
		return NotFoundError{apiError}
	case "RateLimitExceeded":
		return RateLimitExceededError{apiError}
	case "AccessTokenInvalid":
		return AccessTokenInvalidError{apiError}
	case "ValidationFailed", "UnresolvedLinks":
		return ValidationFailedError{apiError}
	case "VersionMismatch":
		return VersionMismatchError{apiError}
	case "Conflict":
		return VersionMismatchError{apiError}
	default:
		return e
	}
}
