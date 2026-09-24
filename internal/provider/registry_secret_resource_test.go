package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestRegistrySecretResourceTypeName(t *testing.T) {
	response := &resource.MetadataResponse{}
	NewRegistrySecretResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "fleets"}, response)
	if response.TypeName != "fleets_registry_secret" {
		t.Fatalf("TypeName = %q, want fleets_registry_secret", response.TypeName)
	}
}

func TestRegistrySecretEveryArgumentForcesReplacement(t *testing.T) {
	// The gateway admits no Secret update, so a changed argument must never
	// reach Update.
	for name, raw := range registrySecretSchema().Attributes {
		if name == "id" {
			continue
		}
		attribute := raw.(schema.StringAttribute)
		if !attribute.Required || len(attribute.PlanModifiers) != 1 {
			t.Fatalf("%s: required = %t, plan modifiers = %d; want required and RequiresReplace", name, attribute.Required, len(attribute.PlanModifiers))
		}
	}
	if password := registrySecretSchema().Attributes["password"].(schema.StringAttribute); !password.Sensitive {
		t.Fatal("password must be sensitive")
	}
}

func TestRegistrySecretNameMatchesGatewayAdmission(t *testing.T) {
	for name, want := range map[string]bool{
		"cua-registry-ghcr":        true,
		"cua-registry-a":           true,
		"cua-registry-my-org-2":    true,
		"cua-registry-":            false,
		"cua-registry--x":          false,
		"cua-registry-X":           false,
		"registry-ghcr":            false,
		"ecr-credentials":          false,
		"cua-registry-ghcr.io":     false,
		"cua-registry-trailing-":   false,
		"prefix-cua-registry-ghcr": false,
	} {
		if got := registrySecretNameRegex.MatchString(name); got != want {
			t.Errorf("%q matches = %t, want %t", name, got, want)
		}
	}
	for host, want := range map[string]bool{
		"ghcr.io":                   true,
		"registry.example.com:5000": true,
		"docker.io":                 true,
		"https://ghcr.io":           false,
		"ghcr.io/org":               false,
		"ghcr io":                   false,
		"registry.example.com:port": false,
	} {
		if got := registryHostRegex.MatchString(host); got != want {
			t.Errorf("registry %q matches = %t, want %t", host, got, want)
		}
	}
}

func TestRegistrySecretModelToSDKRequest(t *testing.T) {
	request := registrySecretModel{
		Namespace: types.StringValue("agent"), Name: types.StringValue("cua-registry-ghcr"),
		Registry: types.StringValue("ghcr.io"), Username: types.StringValue("bot"), Password: types.StringValue("token"),
	}.toSDKRequest()
	if request.Namespace != "agent" || request.Name != "cua-registry-ghcr" || request.Registry != "ghcr.io" || request.Username != "bot" || request.Password != "token" {
		t.Fatalf("request = namespace %q name %q registry %q username %q", request.Namespace, request.Name, request.Registry, request.Username)
	}
}
