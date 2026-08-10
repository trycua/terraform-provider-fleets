//go:build acceptance

package provider_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	fleetsprovider "github.com/trycua/terraform-provider-fleets/internal/provider"
)

type trustPolicyFixture struct {
	ID                string    `json:"id"`
	OwnerSub          string    `json:"owner_sub"`
	Name              string    `json:"name"`
	Repository        string    `json:"repository"`
	AllowedNamespaces []string  `json:"allowed_namespaces"`
	Enabled           bool      `json:"enabled"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func TestAccGitHubTrustPolicyLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	apiServer, policies := newGitHubTrustPolicyTestServer(t)
	defer apiServer.Close()

	providerConfig := fmt.Sprintf(`
provider "fleets" {
  endpoint      = %q
  client_id     = "terraform-e2e"
  client_secret = "terraform-secret"
  token_url     = %q
}
`, apiServer.URL, apiServer.URL+"/token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"fleets": providerserver.NewProtocol6WithError(fleetsprovider.New("test")()),
		},
		CheckDestroy: func(_ *terraform.State) error {
			policies.mu.Lock()
			defer policies.mu.Unlock()
			if len(policies.items) != 0 {
				return fmt.Errorf("trust policies still exist after destroy: %#v", policies.items)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      providerConfig + trustPolicyConfigWithName(" terraform-e2e ", "trycua/cua", true, "alpha"),
				ExpectError: regexp.MustCompile("must not begin or end with whitespace"),
			},
			{
				Config:      providerConfig + trustPolicyConfigForRepository("not-a-repository", true, "alpha"),
				ExpectError: regexp.MustCompile("must use owner/repo format"),
			},
			{
				Config:      providerConfig + trustPolicyConfigForRepository("trycua/cua", true, "Bad_Namespace"),
				ExpectError: regexp.MustCompile(`must be a lowercase DNS\s+label`),
			},
			{
				Config: providerConfig + trustPolicyConfig("alpha", "zeta"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "id", "policy-1"),
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "owner_sub", "owner-1"),
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "repository", "trycua/cua"),
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "allowed_namespaces.#", "2"),
					resource.TestCheckTypeSetElemAttr("fleets_github_trust_policy.test", "allowed_namespaces.*", "alpha"),
					resource.TestCheckTypeSetElemAttr("fleets_github_trust_policy.test", "allowed_namespaces.*", "zeta"),
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "enabled", "true"),
				),
			},
			{
				Config: providerConfig + trustPolicyConfigForRepository("trycua/cua", false, "alpha"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "allowed_namespaces.#", "1"),
					resource.TestCheckResourceAttr("fleets_github_trust_policy.test", "enabled", "false"),
				),
			},
			{
				ResourceName:      "fleets_github_trust_policy.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func trustPolicyConfig(namespaces ...string) string {
	encoded, _ := json.Marshal(namespaces)
	return fmt.Sprintf(`
resource "fleets_github_trust_policy" "test" {
  name               = "terraform-e2e"
  repository         = "trycua/cua"
  allowed_namespaces = %s
}
`, encoded)
}

func trustPolicyConfigForRepository(repository string, enabled bool, namespaces ...string) string {
	return trustPolicyConfigWithName("terraform-e2e", repository, enabled, namespaces...)
}

func trustPolicyConfigWithName(name, repository string, enabled bool, namespaces ...string) string {
	encoded, _ := json.Marshal(namespaces)
	return fmt.Sprintf(`
resource "fleets_github_trust_policy" "test" {
  name               = %q
  repository         = %q
  allowed_namespaces = %s
  enabled            = %t
}
`, name, repository, encoded, enabled)
}

type trustPolicyStore struct {
	mu    sync.Mutex
	items map[string]trustPolicyFixture
}

func newGitHubTrustPolicyTestServer(t *testing.T) (*httptest.Server, *trustPolicyStore) {
	t.Helper()
	store := &trustPolicyStore{items: map[string]trustPolicyFixture{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			clientID, secret, ok := r.BasicAuth()
			if !ok || clientID != "terraform-e2e" || secret != "terraform-secret" {
				http.Error(w, "invalid client credentials", http.StatusUnauthorized)
				return
			}
			writeTrustPolicyJSON(w, http.StatusOK, map[string]any{"access_token": "terraform-e2e-token", "expires_in": 300})
			return
		}
		if r.Header.Get("Authorization") != "Bearer terraform-e2e-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		store.mu.Lock()
		defer store.mu.Unlock()
		now := time.Now().UTC()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/github-trust-policies":
			var input trustPolicyFixture
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			input.ID = "policy-1"
			input.OwnerSub = "owner-1"
			input.CreatedAt = now
			input.UpdatedAt = now
			store.items[input.ID] = input
			writeTrustPolicyJSON(w, http.StatusCreated, input)
		case r.Method == http.MethodGet && r.URL.Path == "/api/github-trust-policies":
			items := make([]trustPolicyFixture, 0, len(store.items))
			for _, item := range store.items {
				items = append(items, item)
			}
			writeTrustPolicyJSON(w, http.StatusOK, map[string]any{"policies": items})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/github-trust-policies/policy-1":
			current, ok := store.items["policy-1"]
			if !ok {
				http.NotFound(w, r)
				return
			}
			var input trustPolicyFixture
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			input.ID = current.ID
			input.OwnerSub = current.OwnerSub
			input.CreatedAt = current.CreatedAt
			input.UpdatedAt = now
			store.items[input.ID] = input
			writeTrustPolicyJSON(w, http.StatusOK, input)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/github-trust-policies/policy-1":
			if _, ok := store.items["policy-1"]; !ok {
				http.NotFound(w, r)
				return
			}
			delete(store.items, "policy-1")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	return server, store
}

func writeTrustPolicyJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
