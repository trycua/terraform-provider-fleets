package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/trycua/terraform-provider-fleets/internal/client"
)

func TestClaimResourceMetadata(t *testing.T) {
	response := &resource.MetadataResponse{}
	NewClaimResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "fleets"}, response)
	if response.TypeName != "fleets_claim" {
		t.Fatalf("TypeName = %q, want fleets_claim", response.TypeName)
	}
}

func TestClaimResourceModelFromClaim(t *testing.T) {
	model := claimResourceModel{CreateTimeout: types.StringValue("10m"), DeleteTimeout: types.StringValue("2m")}
	model.fromClaim("fixed-gvisor", "claim-1", client.Claim{
		Metadata: client.ClaimMetadata{Namespace: "fixed-gvisor"},
		Status: client.ClaimStatus{
			Phase:   "Bound",
			Sandbox: client.ClaimSandbox{Name: "sandbox-1", Service: "sandbox-1.fixed-gvisor.svc.cluster.local"},
		},
	})
	if got := model.ID.ValueString(); got != "fixed-gvisor/claim-1" {
		t.Fatalf("ID = %q", got)
	}
	if got := model.SandboxService.ValueString(); got != "sandbox-1.fixed-gvisor.svc.cluster.local" {
		t.Fatalf("SandboxService = %q", got)
	}
}

func TestClaimFailureMessage(t *testing.T) {
	message := claimFailureMessage(client.Claim{Status: client.ClaimStatus{Conditions: []client.Condition{{Reason: "BindDeadlineExceeded", Message: "no sandbox"}}}})
	if message != "BindDeadlineExceeded: no sandbox" {
		t.Fatalf("message = %q", message)
	}
}

func TestClaimTimeout(t *testing.T) {
	diagnostics := &diag.Diagnostics{}
	if _, ok := claimTimeout(types.StringValue("not-a-duration"), defaultClaimCreateTimeout, "create_timeout", diagnostics); ok || !diagnostics.HasError() {
		t.Fatal("invalid duration should add an error")
	}
}
