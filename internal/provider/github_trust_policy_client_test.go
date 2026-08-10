package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGitHubTrustPolicyClientCRUDWithStaticToken(t *testing.T) {
	createdAt := time.Date(2026, 8, 10, 8, 23, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer static-token" {
			t.Fatalf("authorization = %q", got)
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/github-trust-policies":
			var input githubTrustPolicyInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(input.AllowedNamespaces, []string{"alpha", "zeta"}) {
				t.Fatalf("allowed_namespaces = %#v", input.AllowedNamespaces)
			}
			writeTestJSON(w, http.StatusCreated, githubTrustPolicy{
				ID: "policy-1", OwnerSub: "owner-1", Name: input.Name,
				Repository: input.Repository, AllowedNamespaces: input.AllowedNamespaces,
				Enabled: input.Enabled, CreatedAt: createdAt, UpdatedAt: createdAt,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/github-trust-policies":
			writeTestJSON(w, http.StatusOK, githubTrustPolicyListResponse{Policies: []githubTrustPolicy{{
				ID: "policy-1", OwnerSub: "owner-1", Name: "smoke", Repository: "trycua/cua",
				AllowedNamespaces: []string{"alpha", "zeta"}, Enabled: true,
				CreatedAt: createdAt, UpdatedAt: createdAt,
			}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/github-trust-policies/policy-1":
			writeTestJSON(w, http.StatusOK, githubTrustPolicy{
				ID: "policy-1", OwnerSub: "owner-1", Name: "smoke", Repository: "trycua/cua",
				AllowedNamespaces: []string{"alpha"}, Enabled: false,
				CreatedAt: createdAt, UpdatedAt: updatedAt,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/github-trust-policies/policy-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := newGitHubTrustPolicyClient(server.URL, "static-token", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	input := githubTrustPolicyInput{
		Name: "smoke", Repository: "trycua/cua",
		AllowedNamespaces: []string{"alpha", "zeta"}, Enabled: true,
	}
	created, err := client.create(context.Background(), input)
	if err != nil || created.ID != "policy-1" {
		t.Fatalf("create = %#v, %v", created, err)
	}
	policies, err := client.list(context.Background())
	if err != nil || len(policies) != 1 {
		t.Fatalf("list = %#v, %v", policies, err)
	}
	updated, err := client.update(context.Background(), "policy-1", githubTrustPolicyInput{
		Name: "smoke", Repository: "trycua/cua", AllowedNamespaces: []string{"alpha"}, Enabled: false,
	})
	if err != nil || updated.Enabled || !updated.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("update = %#v, %v", updated, err)
	}
	if err := client.delete(context.Background(), "policy-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /api/github-trust-policies",
		"GET /api/github-trust-policies",
		"PATCH /api/github-trust-policies/policy-1",
		"DELETE /api/github-trust-policies/policy-1",
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestGitHubTrustPolicyClientCachesOAuthToken(t *testing.T) {
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenRequests++
			clientID, clientSecret, ok := r.BasicAuth()
			if !ok || clientID != "client" || clientSecret != "secret" {
				t.Fatalf("unexpected basic auth: %q %q %t", clientID, clientSecret, ok)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if got := r.Form.Get("grant_type"); got != "client_credentials" {
				t.Fatalf("grant_type = %q", got)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"access_token": "oauth-token", "expires_in": 300})
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("authorization = %q", got)
		}
		writeTestJSON(w, http.StatusOK, githubTrustPolicyListResponse{Policies: []githubTrustPolicy{}})
	}))
	defer server.Close()

	client, err := newGitHubTrustPolicyClient(server.URL, "", "client", "secret", server.URL+"/token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.list(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.list(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tokenRequests != 1 {
		t.Fatalf("token requests = %d, want 1", tokenRequests)
	}
}

func TestGitHubTrustPolicyClientReportsAPIErrorWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"denied"}`, http.StatusForbidden)
	}))
	defer server.Close()

	client, err := newGitHubTrustPolicyClient(server.URL, "static-token", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.list(context.Background())
	if err == nil || err.Error() != "Fleets API GET /api/github-trust-policies returned 403: {\"error\":\"denied\"}" {
		t.Fatalf("error = %v", err)
	}
	if got := err.Error(); strings.Contains(got, "static-token") {
		t.Fatal("error exposed bearer token")
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}

func TestIsFleetsAPINotFoundRecognizesWrappedErrors(t *testing.T) {
	err := fmt.Errorf("read policy: %w", &fleetsAPIError{Status: http.StatusNotFound})
	if !isFleetsAPINotFound(err) {
		t.Fatal("wrapped 404 was not recognized")
	}
	if isFleetsAPINotFound(&fleetsAPIError{Status: http.StatusForbidden}) {
		t.Fatal("non-404 was recognized as not found")
	}
}
