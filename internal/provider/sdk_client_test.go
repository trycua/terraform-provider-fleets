package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func TestNewCyclopsClientUsesStaticAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer static-token" {
			t.Fatalf("authorization = %q, want static bearer token", got)
		}
		if r.URL.Path != "/api/k8s/apis/osgym.cua.ai/v1alpha1/namespaces/demo/osgymsandboxwarmpools" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer server.Close()

	client, err := newCyclopsClient(server.URL, "static-token", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Destroy()
	if _, err := client.ListPools("demo"); err != nil {
		t.Fatal(err)
	}
}

func TestNewCyclopsClientUsesOAuthCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			clientID, clientSecret, ok := r.BasicAuth()
			if !ok || clientID != "client" || clientSecret != "secret" {
				t.Fatalf("unexpected OAuth credentials: %q %q %t", clientID, clientSecret, ok)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "oauth-token", "expires_in": 300})
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("authorization = %q, want OAuth bearer token", got)
		}
		if r.URL.Path != "/api/k8s/apis/osgym.cua.ai/v1alpha1/namespaces/demo/osgymsandboxwarmpools" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer server.Close()

	client, err := newCyclopsClient(server.URL, "", "client", "secret", server.URL+"/token")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Destroy()
	if _, err := client.ListPools("demo"); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileTemplateSwitchesBackToDefaultRuntimeAndFirmware(t *testing.T) {
	const templatePath = "/api/k8s/apis/osgym.cua.ai/v1alpha1/namespaces/example/osgymsandboxtemplates/example-template"
	var patch map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != templatePath {
			t.Fatalf("path = %q", r.URL.Path)
		}
		stored := map[string]any{
			"apiVersion": "osgym.cua.ai/v1alpha1", "kind": "OSGymSandboxTemplate",
			"metadata": map[string]any{"namespace": "example", "name": "example-template"},
			"spec": map[string]any{"vmTemplate": map[string]any{
				"containerDiskImage": "registry.example/image:latest", "runtime": "gvisor", "firmware": "efi",
			}},
		}
		if r.Method == http.MethodPatch {
			if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
				t.Fatal(err)
			}
		}
		_ = json.NewEncoder(w).Encode(stored)
	}))
	defer server.Close()

	client, err := newCyclopsClient(server.URL, "static-token", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Destroy()

	// The stored template runs gVisor with EFI; the configuration asks for the
	// defaults again.
	model := examplePoolModel()
	var diagnostics diag.Diagnostics
	request := model.toSDKCreateTemplateRequest(context.Background(), &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if _, err := client.ReconcileTemplate(request); err != nil {
		t.Fatal(err)
	}
	vmTemplate := patch["spec"].(map[string]any)["vmTemplate"].(map[string]any)
	if vmTemplate["runtime"] != "kubevirt" {
		t.Fatalf("patched runtime = %v, want kubevirt", vmTemplate["runtime"])
	}
	if vmTemplate["firmware"] != "bios" {
		t.Fatalf("patched firmware = %v, want bios", vmTemplate["firmware"])
	}
}
