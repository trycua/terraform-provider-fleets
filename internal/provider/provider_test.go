package provider

import (
	"context"
	"testing"

	frameworkprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestMetadataUsesFleetsTypeName(t *testing.T) {
	instance := New("1.0.0")()
	response := &frameworkprovider.MetadataResponse{}
	instance.Metadata(context.Background(), frameworkprovider.MetadataRequest{}, response)

	if response.TypeName != "fleets" {
		t.Fatalf("TypeName = %q, want fleets", response.TypeName)
	}
	if response.Version != "1.0.0" {
		t.Fatalf("Version = %q, want 1.0.0", response.Version)
	}
}

func TestResourceMetadata(t *testing.T) {
	tests := []struct {
		name     string
		resource resource.Resource
		want     string
	}{
		{name: "current", resource: NewPoolResource(), want: "fleets_pool"},
		{name: "legacy", resource: NewLegacyPoolResource(), want: "cyclops_pool"},
		{name: "github trust policy", resource: NewGitHubTrustPolicyResource(), want: "fleets_github_trust_policy"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &resource.MetadataResponse{}
			test.resource.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "fleets"}, response)
			if response.TypeName != test.want {
				t.Fatalf("TypeName = %q, want %q", response.TypeName, test.want)
			}
		})
	}
}

func TestProviderProtocolSchemaIsValid(t *testing.T) {
	server, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatal(err)
	}
	if server == nil {
		t.Fatal("provider server is nil")
	}
}
