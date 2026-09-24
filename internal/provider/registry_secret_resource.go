package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/trycua/cloud/cyclops-cs/sdk-bindings/go-uniffi/fleet_sdk"
)

// registrySecretNameRegex mirrors the gateway's tenant Secret admission
// (backend/auth/tenant_secret_admission.rego): tenants may only create
// `cua-registry-<dns-label>` dockerconfigjson Secrets.
var registrySecretNameRegex = regexp.MustCompile(`^cua-registry-[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// registryHostRegex is a bare host[:port], as image refs spell it.
var registryHostRegex = regexp.MustCompile(`^[^/\s:]+(:[0-9]+)?$`)

type registrySecretModel struct {
	ID        types.String `tfsdk:"id"`
	Namespace types.String `tfsdk:"namespace"`
	Name      types.String `tfsdk:"name"`
	Registry  types.String `tfsdk:"registry"`
	Username  types.String `tfsdk:"username"`
	Password  types.String `tfsdk:"password"`
}

// registrySecretResource manages a tenant registry pull Secret. The gateway
// admits only create and delete on these Secrets (no read, list or update),
// so the resource is write-only: every argument forces replacement and Read
// keeps the state it wrote.
type registrySecretResource struct {
	client *fleet_sdk.CyclopsClient
}

func NewRegistrySecretResource() resource.Resource { return &registrySecretResource{} }

func (r *registrySecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_secret"
}

func registrySecretSchema() schema.Schema {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	return schema.Schema{
		Description: "A private-registry pull Secret in a Fleet pool namespace: a kubernetes.io/dockerconfigjson Secret named cua-registry-<name> and labeled cua.ai/registry-secret=true. " +
			"Reference it from fleets_pool.image_pull_secret. Fleet never returns Secret contents, so the resource is write-only: changing any argument replaces the Secret, " +
			"and a Secret deleted outside Terraform is not detected.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "<namespace>/<name>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"namespace": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Description: "Pool namespace (the fleets_pool name). It must already exist, so reference fleets_pool.<name>.namespace.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 63), stringvalidator.RegexMatches(dnsLabelRegex, "must be a lowercase DNS label")},
			},
			"name": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Description: "Secret name, cua-registry-<dns-label>.",
				Validators:  []validator.String{stringvalidator.LengthAtMost(253), stringvalidator.RegexMatches(registrySecretNameRegex, "must be cua-registry-<dns-label>")},
			},
			"registry": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Description: "Registry host the credentials are for, as image refs spell it: ghcr.io, registry.example.com:5000 or docker.io.",
				Validators:  []validator.String{stringvalidator.LengthBetween(1, 255), stringvalidator.RegexMatches(registryHostRegex, "must be a bare host[:port] such as ghcr.io")},
			},
			"username": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"password": schema.StringAttribute{
				Required: true, Sensitive: true, PlanModifiers: replace,
				Description: "Password or access token. Stored in Terraform state (marked sensitive) and sent only on create.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *registrySecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = registrySecretSchema()
}

func (r *registrySecretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*fleet_sdk.CyclopsClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *fleet_sdk.CyclopsClient, got %T", req.ProviderData))
		return
	}
	r.client = apiClient
}

func (r *registrySecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registrySecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	created, err := r.client.CreateRegistrySecret(plan.toSDKRequest())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create Fleet registry secret", err.Error())
		return
	}
	plan.ID = types.StringValue(created.Namespace + "/" + created.Name)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read keeps the state: the gateway refuses GET on Secrets, so there is
// nothing to refresh from.
func (r *registrySecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registrySecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never reached with a change (every argument requires
// replacement); it only carries the planned state forward.
func (r *registrySecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan registrySecretModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *registrySecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state registrySecretModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The SDK treats an already-deleted Secret (404) as success.
	if err := r.client.DeleteRegistrySecret(state.Namespace.ValueString(), state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to delete Fleet registry secret", err.Error())
	}
}

func (m registrySecretModel) toSDKRequest() fleet_sdk.CreateRegistrySecretRequest {
	return fleet_sdk.CreateRegistrySecretRequest{
		Namespace: m.Namespace.ValueString(),
		Name:      m.Name.ValueString(),
		Registry:  m.Registry.ValueString(),
		Username:  m.Username.ValueString(),
		Password:  m.Password.ValueString(),
	}
}

var _ resource.Resource = (*registrySecretResource)(nil)
var _ resource.ResourceWithConfigure = (*registrySecretResource)(nil)
