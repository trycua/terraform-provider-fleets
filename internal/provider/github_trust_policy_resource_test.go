package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestGitHubTrustPolicyResourceMetadata(t *testing.T) {
	response := &resource.MetadataResponse{}
	NewGitHubTrustPolicyResource().Metadata(
		context.Background(), resource.MetadataRequest{ProviderTypeName: "fleets"}, response,
	)
	if response.TypeName != "fleets_github_trust_policy" {
		t.Fatalf("TypeName = %q", response.TypeName)
	}
}

func TestGitHubTrustPolicyModelInputSortsNamespaces(t *testing.T) {
	namespaces, diagnostics := types.SetValueFrom(context.Background(), types.StringType, []string{"zeta", "alpha"})
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	model := githubTrustPolicyResourceModel{
		Name: types.StringValue("smoke"), Repository: types.StringValue("trycua/cua"),
		AllowedNamespaces: namespaces, Enabled: types.BoolValue(true),
	}
	var conversionDiagnostics diag.Diagnostics
	input := model.toAPIInput(context.Background(), &conversionDiagnostics)
	if conversionDiagnostics.HasError() {
		t.Fatal(conversionDiagnostics)
	}
	if !reflect.DeepEqual(input.AllowedNamespaces, []string{"alpha", "zeta"}) {
		t.Fatalf("allowed_namespaces = %#v", input.AllowedNamespaces)
	}
}

func TestGitHubTrustPolicyModelFromAPI(t *testing.T) {
	createdAt := time.Date(2026, 8, 10, 8, 23, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	policy := githubTrustPolicy{
		ID: "policy-1", OwnerSub: "owner-1", Name: "smoke", Repository: "trycua/cua",
		AllowedNamespaces: []string{"alpha", "zeta"}, Enabled: true,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}
	var model githubTrustPolicyResourceModel
	var diagnostics diag.Diagnostics
	model.fromAPI(context.Background(), policy, &diagnostics)
	if diagnostics.HasError() {
		t.Fatal(diagnostics)
	}
	if model.ID.ValueString() != "policy-1" || model.OwnerSub.ValueString() != "owner-1" {
		t.Fatalf("identity = %q %q", model.ID.ValueString(), model.OwnerSub.ValueString())
	}
	if model.CreatedAt.ValueString() != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("created_at = %q", model.CreatedAt.ValueString())
	}
	var namespaces []string
	diagnostics.Append(model.AllowedNamespaces.ElementsAs(context.Background(), &namespaces, false)...)
	if diagnostics.HasError() || !reflect.DeepEqual(namespaces, []string{"alpha", "zeta"}) {
		t.Fatalf("namespaces = %#v, diagnostics = %v", namespaces, diagnostics)
	}
}
