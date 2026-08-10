package provider

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	repositoryRegex  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	trimmedNameRegex = regexp.MustCompile(`^\S(?:.*\S)?$`)
)

type githubTrustPolicyResource struct {
	client *githubTrustPolicyClient
}

type githubTrustPolicyResourceModel struct {
	ID                types.String `tfsdk:"id"`
	OwnerSub          types.String `tfsdk:"owner_sub"`
	Name              types.String `tfsdk:"name"`
	Repository        types.String `tfsdk:"repository"`
	AllowedNamespaces types.Set    `tfsdk:"allowed_namespaces"`
	Enabled           types.Bool   `tfsdk:"enabled"`
	CreatedAt         types.String `tfsdk:"created_at"`
	UpdatedAt         types.String `tfsdk:"updated_at"`
}

func NewGitHubTrustPolicyResource() resource.Resource {
	return &githubTrustPolicyResource{}
}

func (r *githubTrustPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_github_trust_policy"
}

func (r *githubTrustPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A GitHub Actions OIDC trust policy granting one repository access to exact Fleet namespaces.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Computed: true, Description: "Trust policy ID."},
			"owner_sub": schema.StringAttribute{Computed: true, Description: "Owner identity derived from the provider credentials."},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable trust policy name.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.RegexMatches(trimmedNameRegex, "must not begin or end with whitespace"),
				},
			},
			"repository": schema.StringAttribute{
				Required: true, Description: "Exact GitHub repository claim in owner/repo format.",
				Validators: []validator.String{stringvalidator.RegexMatches(repositoryRegex, "must use owner/repo format")},
			},
			"allowed_namespaces": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				Description: "Exact Fleet namespace DNS labels granted to the repository.",
				Validators: []validator.Set{
					setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(
						stringvalidator.LengthBetween(1, 63),
						stringvalidator.RegexMatches(dnsLabelRegex, "must be a lowercase DNS label"),
					),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether this trust policy participates in GitHub OIDC authorization.",
			},
			"created_at": schema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at": schema.StringAttribute{Computed: true, Description: "RFC3339 last-update timestamp."},
		},
	}
}

func (r *githubTrustPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*configuredClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *configuredClient, got %T", req.ProviderData))
		return
	}
	r.client = client.githubTrustPolicies
}

func (r *githubTrustPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan githubTrustPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input := plan.toAPIInput(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.create(ctx, input)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create GitHub trust policy", err.Error())
		return
	}
	plan.fromAPI(ctx, *policy, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *githubTrustPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state githubTrustPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	policies, err := r.client.list(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read GitHub trust policy", err.Error())
		return
	}
	for _, policy := range policies {
		if policy.ID == state.ID.ValueString() {
			state.fromAPI(ctx, policy, &resp.Diagnostics)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *githubTrustPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan githubTrustPolicyResourceModel
	var state githubTrustPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	input := plan.toAPIInput(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	policy, err := r.client.update(ctx, state.ID.ValueString(), input)
	if err != nil {
		if isFleetsAPINotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to update GitHub trust policy", err.Error())
		return
	}
	plan.fromAPI(ctx, *policy, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *githubTrustPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state githubTrustPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.delete(ctx, state.ID.ValueString()); err != nil && !isFleetsAPINotFound(err) {
		resp.Diagnostics.AddError("Unable to delete GitHub trust policy", err.Error())
	}
}

func (r *githubTrustPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (m githubTrustPolicyResourceModel) toAPIInput(ctx context.Context, diagnostics *diag.Diagnostics) githubTrustPolicyInput {
	var namespaces []string
	diagnostics.Append(m.AllowedNamespaces.ElementsAs(ctx, &namespaces, false)...)
	slices.Sort(namespaces)
	return githubTrustPolicyInput{
		Name: m.Name.ValueString(), Repository: m.Repository.ValueString(),
		AllowedNamespaces: namespaces, Enabled: m.Enabled.ValueBool(),
	}
}

func (m *githubTrustPolicyResourceModel) fromAPI(ctx context.Context, policy githubTrustPolicy, diagnostics *diag.Diagnostics) {
	namespaces, namespaceDiagnostics := types.SetValueFrom(ctx, types.StringType, policy.AllowedNamespaces)
	diagnostics.Append(namespaceDiagnostics...)
	m.ID = types.StringValue(policy.ID)
	m.OwnerSub = types.StringValue(policy.OwnerSub)
	m.Name = types.StringValue(policy.Name)
	m.Repository = types.StringValue(policy.Repository)
	m.AllowedNamespaces = namespaces
	m.Enabled = types.BoolValue(policy.Enabled)
	m.CreatedAt = types.StringValue(policy.CreatedAt.Format(time.RFC3339Nano))
	m.UpdatedAt = types.StringValue(policy.UpdatedAt.Format(time.RFC3339Nano))
}

var _ resource.Resource = (*githubTrustPolicyResource)(nil)
var _ resource.ResourceWithConfigure = (*githubTrustPolicyResource)(nil)
var _ resource.ResourceWithImportState = (*githubTrustPolicyResource)(nil)
