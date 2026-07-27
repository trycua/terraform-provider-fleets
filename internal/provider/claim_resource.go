package provider

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/trycua/terraform-provider-fleets/internal/client"
)

const (
	defaultClaimCreateTimeout = 10 * time.Minute
	defaultClaimDeleteTimeout = 2 * time.Minute
)

var claimPollInterval = time.Second

type claimResource struct {
	client *client.Client
}

type claimResourceModel struct {
	ID             types.String `tfsdk:"id"`
	PoolName       types.String `tfsdk:"pool_name"`
	ClaimName      types.String `tfsdk:"claim_name"`
	Namespace      types.String `tfsdk:"namespace"`
	Phase          types.String `tfsdk:"phase"`
	SandboxName    types.String `tfsdk:"sandbox_name"`
	SandboxService types.String `tfsdk:"sandbox_service"`
	CreateTimeout  types.String `tfsdk:"create_timeout"`
	DeleteTimeout  types.String `tfsdk:"delete_timeout"`
}

func NewClaimResource() resource.Resource { return &claimResource{} }

func (r *claimResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claim"
}

func (r *claimResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Claims exactly one sandbox from an existing Fleet pool.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Stable claim identifier: pool_name/claim_name."},
			"pool_name": schema.StringAttribute{
				Required: true, Description: "Pool name and claim namespace.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 63), stringvalidator.RegexMatches(dnsLabelRegex, "must be a lowercase DNS label")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"claim_name": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Claim DNS label. The provider generates a unique name when omitted.",
				Validators:    []validator.String{stringvalidator.LengthBetween(1, 63), stringvalidator.RegexMatches(dnsLabelRegex, "must be a lowercase DNS label")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()},
			},
			"namespace":       schema.StringAttribute{Computed: true, Description: "Namespace containing the claim; equal to pool_name."},
			"phase":           schema.StringAttribute{Computed: true, Description: "Claim lifecycle phase reported by Fleet: Pending, Bound, or Failed."},
			"sandbox_name":    schema.StringAttribute{Computed: true, Description: "Name of the sandbox bound to this claim when phase is Bound."},
			"sandbox_service": schema.StringAttribute{Computed: true, Description: "Authoritative in-cluster Service DNS reported by the claim. This is not a public gateway URL."},
			"create_timeout": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Maximum duration to create and wait for the claim to become Bound.",
				Default:    stringdefault.StaticString(defaultClaimCreateTimeout.String()),
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"delete_timeout": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Maximum duration to release the claim and observe its deletion.",
				Default:    stringdefault.StaticString(defaultClaimDeleteTimeout.String()),
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *claimResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	apiClient, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", req.ProviderData))
		return
	}
	r.client = apiClient
}

func (r *claimResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan claimResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	createTimeout, ok := claimTimeout(plan.CreateTimeout, defaultClaimCreateTimeout, "create_timeout", &resp.Diagnostics)
	if !ok {
		return
	}
	claimName := plan.ClaimName.ValueString()
	if claimName == "" {
		var err error
		claimName, err = generatedClaimName()
		if err != nil {
			resp.Diagnostics.AddError("Unable to generate claim name", err.Error())
			return
		}
		plan.ClaimName = types.StringValue(claimName)
	}
	poolName := plan.PoolName.ValueString()
	createContext, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()
	if err := retryTransient(createContext, func() error {
		_, err := r.client.CreateClaim(createContext, poolName, claimName)
		return err
	}); err != nil {
		resp.Diagnostics.AddError("Unable to create Fleet claim", err.Error())
		return
	}
	claim, err := r.waitForBound(createContext, poolName, claimName)
	if err != nil {
		r.releaseAfterCreateFailure(plan, poolName, claimName, &resp.Diagnostics)
		resp.Diagnostics.AddError("Unable to bind Fleet claim", err.Error())
		return
	}
	plan.fromClaim(poolName, claimName, claim)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *claimResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state claimResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	claim, err := r.client.GetClaim(ctx, state.PoolName.ValueString(), state.ClaimName.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Fleet claim", err.Error())
		return
	}
	state.fromClaim(state.PoolName.ValueString(), state.ClaimName.ValueString(), claim)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *claimResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan claimResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	claim, err := r.client.GetClaim(ctx, plan.PoolName.ValueString(), plan.ClaimName.ValueString())
	if client.IsNotFound(err) {
		resp.Diagnostics.AddError("Fleet claim no longer exists", "The claim was released outside Terraform. Run terraform apply to create a replacement.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read Fleet claim", err.Error())
		return
	}
	plan.fromClaim(plan.PoolName.ValueString(), plan.ClaimName.ValueString(), claim)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *claimResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state claimResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	deleteTimeout, ok := claimTimeout(state.DeleteTimeout, defaultClaimDeleteTimeout, "delete_timeout", &resp.Diagnostics)
	if !ok {
		return
	}
	deleteContext, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()
	if err := r.releaseClaim(deleteContext, state.PoolName.ValueString(), state.ClaimName.ValueString()); err != nil {
		resp.Diagnostics.AddError("Unable to release Fleet claim", err.Error())
	}
}

func (r *claimResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	poolName, claimName, ok := strings.Cut(req.ID, "/")
	if !ok || poolName == "" || claimName == "" || strings.Contains(claimName, "/") {
		resp.Diagnostics.AddError("Invalid Fleet claim import ID", "Use pool_name/claim_name, for example: terraform import fleets_claim.example fixed-gvisor/claim-abc123.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("pool_name"), poolName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("claim_name"), claimName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), poolName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("create_timeout"), defaultClaimCreateTimeout.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("delete_timeout"), defaultClaimDeleteTimeout.String())...)
}

func (r *claimResource) waitForBound(ctx context.Context, poolName, claimName string) (client.Claim, error) {
	for {
		claim, err := r.client.GetClaim(ctx, poolName, claimName)
		if err == nil {
			switch claim.Status.Phase {
			case "Bound":
				if claim.Status.Sandbox.Name != "" {
					return claim, nil
				}
				return client.Claim{}, errors.New("claim reports Bound without a sandbox name")
			case "Failed":
				return client.Claim{}, fmt.Errorf("claim failed: %s", claimFailureMessage(claim))
			}
		} else if !client.IsTransient(err) {
			return client.Claim{}, err
		}
		if err := waitForPoll(ctx); err != nil {
			return client.Claim{}, fmt.Errorf("timed out waiting for claim %s/%s to bind: %w", poolName, claimName, err)
		}
	}
}

func (r *claimResource) releaseClaim(ctx context.Context, poolName, claimName string) error {
	if err := retryTransient(ctx, func() error { return r.client.DeleteClaim(ctx, poolName, claimName) }); err != nil {
		return err
	}
	for {
		_, err := r.client.GetClaim(ctx, poolName, claimName)
		if client.IsNotFound(err) {
			return nil
		}
		if err != nil && !client.IsTransient(err) {
			return err
		}
		if err := waitForPoll(ctx); err != nil {
			return fmt.Errorf("timed out waiting for claim %s/%s to release: %w", poolName, claimName, err)
		}
	}
}

func (r *claimResource) releaseAfterCreateFailure(plan claimResourceModel, poolName, claimName string, diagnostics *diag.Diagnostics) {
	deleteTimeout, ok := claimTimeout(plan.DeleteTimeout, defaultClaimDeleteTimeout, "delete_timeout", diagnostics)
	if !ok {
		return
	}
	cleanupContext, cancel := context.WithTimeout(context.Background(), deleteTimeout)
	defer cancel()
	if err := r.releaseClaim(cleanupContext, poolName, claimName); err != nil {
		diagnostics.AddWarning("Fleet claim cleanup failed", fmt.Sprintf("Claim %s/%s could not be released after create failed: %s", poolName, claimName, err))
	}
}

func (m *claimResourceModel) fromClaim(poolName, claimName string, claim client.Claim) {
	namespace := claim.Metadata.Namespace
	if namespace == "" {
		namespace = poolName
	}
	m.ID = types.StringValue(namespace + "/" + claimName)
	m.PoolName = types.StringValue(poolName)
	m.ClaimName = types.StringValue(claimName)
	m.Namespace = types.StringValue(namespace)
	m.Phase = types.StringValue(claim.Status.Phase)
	m.SandboxName = stringOrNull(claim.Status.Sandbox.Name)
	m.SandboxService = stringOrNull(claim.Status.Sandbox.Service)
}

func claimTimeout(value types.String, fallback time.Duration, field string, diagnostics *diag.Diagnostics) (time.Duration, bool) {
	text := value.ValueString()
	if text == "" {
		return fallback, true
	}
	duration, err := time.ParseDuration(text)
	if err != nil || duration <= 0 {
		diagnostics.AddError("Invalid "+field, "Set "+field+" to a positive Go duration such as 10m.")
		return 0, false
	}
	return duration, true
}

func generatedClaimName() (string, error) {
	bytes := make([]byte, 8)
	if _, err := cryptorand.Read(bytes); err != nil {
		return "", err
	}
	return "tf-claim-" + hex.EncodeToString(bytes), nil
}

func retryTransient(ctx context.Context, operation func() error) error {
	for delay := 200 * time.Millisecond; ; delay = minDuration(delay*2, 5*time.Second) {
		err := operation()
		if err == nil || !client.IsTransient(err) {
			return err
		}
		if err := waitFor(ctx, delay); err != nil {
			return fmt.Errorf("retry deadline exceeded: %w", err)
		}
	}
}

func waitForPoll(ctx context.Context) error { return waitFor(ctx, claimPollInterval) }

func waitFor(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func claimFailureMessage(claim client.Claim) string {
	if len(claim.Status.Conditions) == 0 {
		return "no failure condition was reported"
	}
	condition := claim.Status.Conditions[0]
	parts := []string{condition.Reason, condition.Message}
	message := strings.TrimSpace(strings.Join(parts, ": "))
	if message == ":" || message == "" {
		return "no failure detail was reported"
	}
	return strings.Trim(message, ": ")
}

func stringOrNull(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

var _ resource.Resource = (*claimResource)(nil)
var _ resource.ResourceWithConfigure = (*claimResource)(nil)
var _ resource.ResourceWithImportState = (*claimResource)(nil)
