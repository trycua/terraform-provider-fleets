package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/trycua/cloud/cyclops-cs/sdk-bindings/go-uniffi/fleet_sdk"
)

type configuredClient struct {
	*fleet_sdk.CyclopsClient
	githubTrustPolicies *githubTrustPolicyClient
}

func newConfiguredClient(endpoint, accessToken, clientID, clientSecret, tokenURL string) (*configuredClient, error) {
	fleetClient, err := newCyclopsClient(endpoint, accessToken, clientID, clientSecret, tokenURL)
	if err != nil {
		return nil, err
	}
	trustPolicyClient, err := newGitHubTrustPolicyClient(endpoint, accessToken, clientID, clientSecret, tokenURL)
	if err != nil {
		fleetClient.Destroy()
		return nil, err
	}
	return &configuredClient{CyclopsClient: fleetClient, githubTrustPolicies: trustPolicyClient}, nil
}

type githubTrustPolicy struct {
	ID                string    `json:"id"`
	OwnerSub          string    `json:"owner_sub"`
	Name              string    `json:"name"`
	Repository        string    `json:"repository"`
	AllowedNamespaces []string  `json:"allowed_namespaces"`
	Enabled           bool      `json:"enabled"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type githubTrustPolicyInput struct {
	Name              string   `json:"name"`
	Repository        string   `json:"repository"`
	AllowedNamespaces []string `json:"allowed_namespaces"`
	Enabled           bool     `json:"enabled"`
}

type githubTrustPolicyListResponse struct {
	Policies []githubTrustPolicy `json:"policies"`
}

type githubTrustPolicyClient struct {
	endpoint   string
	httpClient *http.Client
	tokens     *bearerTokenSource
}

func newGitHubTrustPolicyClient(endpoint, accessToken, clientID, clientSecret, tokenURL string) (*githubTrustPolicyClient, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if accessToken == "" && (clientID == "" || clientSecret == "" || tokenURL == "") {
		return nil, fmt.Errorf("set access_token or client_id, client_secret, and token_url")
	}
	return &githubTrustPolicyClient{
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokens: &bearerTokenSource{
			accessToken: accessToken,
			clientID:    clientID, clientSecret: clientSecret, tokenURL: tokenURL,
			httpClient: &http.Client{Timeout: 30 * time.Second},
		},
	}, nil
}

func (c *githubTrustPolicyClient) create(ctx context.Context, input githubTrustPolicyInput) (*githubTrustPolicy, error) {
	var policy githubTrustPolicy
	if err := c.doJSON(ctx, http.MethodPost, "/api/github-trust-policies", input, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (c *githubTrustPolicyClient) list(ctx context.Context) ([]githubTrustPolicy, error) {
	var response githubTrustPolicyListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/github-trust-policies", nil, &response); err != nil {
		return nil, err
	}
	return response.Policies, nil
}

func (c *githubTrustPolicyClient) update(ctx context.Context, id string, input githubTrustPolicyInput) (*githubTrustPolicy, error) {
	var policy githubTrustPolicy
	path := "/api/github-trust-policies/" + url.PathEscape(id)
	if err := c.doJSON(ctx, http.MethodPatch, path, input, &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (c *githubTrustPolicyClient) delete(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/github-trust-policies/"+url.PathEscape(id), nil, nil)
}

func (c *githubTrustPolicyClient) doJSON(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return fmt.Errorf("encode Fleets API request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, body)
	if err != nil {
		return fmt.Errorf("create Fleets API request: %w", err)
	}
	token, err := c.tokens.token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call Fleets API %s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read Fleets API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &fleetsAPIError{Method: method, Path: path, Status: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	if output == nil || len(responseBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return fmt.Errorf("decode Fleets API response: %w", err)
	}
	return nil
}

type fleetsAPIError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *fleetsAPIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Fleets API %s %s returned %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("Fleets API %s %s returned %d: %s", e.Method, e.Path, e.Status, e.Body)
}

func isFleetsAPINotFound(err error) bool {
	var apiError *fleetsAPIError
	return errors.As(err, &apiError) && apiError.Status == http.StatusNotFound
}

type bearerTokenSource struct {
	accessToken  string
	clientID     string
	clientSecret string
	tokenURL     string
	httpClient   *http.Client

	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

func (s *bearerTokenSource) token(ctx context.Context) (string, error) {
	if s.accessToken != "" {
		return s.accessToken, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != "" && time.Now().Before(s.expiresAt.Add(-30*time.Second)) {
		return s.cached, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create OAuth token request: %w", err)
	}
	req.SetBasicAuth(s.clientID, s.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request OAuth token: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read OAuth token response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("OAuth token endpoint returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", fmt.Errorf("decode OAuth token response: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return "", fmt.Errorf("OAuth token response did not include access_token")
	}
	if tokenResponse.ExpiresIn <= 0 {
		tokenResponse.ExpiresIn = 300
	}
	s.cached = tokenResponse.AccessToken
	s.expiresAt = time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	return s.cached, nil
}
