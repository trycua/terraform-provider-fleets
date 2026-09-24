package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/trycua/cloud/cyclops-cs/sdk-bindings/go-uniffi/cyclops_sdk_schema"
	"github.com/trycua/cloud/cyclops-cs/sdk-bindings/go-uniffi/fleet_sdk"
)

func testSidecars() types.List {
	return types.ListValueMust(types.ObjectType{AttrTypes: sidecarObjectType()}, []attr.Value{
		types.ObjectValueMust(sidecarObjectType(), map[string]attr.Value{
			"name":    types.StringValue("redis"),
			"image":   types.StringValue("redis:7"),
			"command": types.ListNull(types.StringType),
			"args":    types.ListValueMust(types.StringType, []attr.Value{types.StringValue("--save"), types.StringValue("")}),
			"env":     types.MapValueMust(types.StringType, map[string]attr.Value{"MODE": types.StringValue("cache")}),
			"ports":   types.ListValueMust(types.Int64Type, []attr.Value{types.Int64Value(6379)}),
			"cpu":     types.StringValue("250m"),
			"memory":  types.StringNull(),
		}),
	})
}

func processPoolModel() poolResourceModel {
	model := parityPoolModel()
	model.Args = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("--port"), types.StringValue("8765")})
	model.Env = types.MapValueMust(types.StringType, map[string]attr.Value{"FOO": types.StringValue("bar")})
	model.ProcessMode = types.StringValue("Run")
	model.Sidecars = testSidecars()
	return model
}

func TestPoolProcessAttributesAreOptionalOnly(t *testing.T) {
	attributes := poolResourceSchema().Attributes
	for _, name := range []string{"args", "env", "process_mode"} {
		attribute := attributes[name]
		if attribute == nil || !attribute.IsOptional() || attribute.IsComputed() {
			t.Fatalf("%s must be optional-only", name)
		}
	}
	if _, ok := poolResourceSchema().Blocks["sidecar"]; !ok {
		t.Fatal("sidecar block is missing from the schema")
	}
}

func TestPoolProcessAttributesReachTheSDK(t *testing.T) {
	model := processPoolModel()
	var diagnostics diag.Diagnostics
	vm := model.toSDKTemplateSpec(context.Background(), &diagnostics).VmTemplate
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if vm.Args == nil || len(*vm.Args) != 2 || (*vm.Args)[1] != "8765" {
		t.Fatalf("args = %v", vm.Args)
	}
	if vm.Env == nil || (*vm.Env)["FOO"] != "bar" {
		t.Fatalf("env = %v", vm.Env)
	}
	if vm.ProcessMode == nil || *vm.ProcessMode != cyclops_sdk_schema.ProcessModeRun {
		t.Fatalf("processMode = %v, want Run", vm.ProcessMode)
	}
	if vm.Sidecars == nil || len(*vm.Sidecars) != 1 {
		t.Fatalf("sidecars = %v, want one", vm.Sidecars)
	}
	sidecar := (*vm.Sidecars)[0]
	if sidecar.Name != "redis" || sidecar.Image != "redis:7" || sidecar.Command != nil || sidecar.Memory != nil {
		t.Fatalf("sidecar = %+v", sidecar)
	}
	if sidecar.Args == nil || len(*sidecar.Args) != 2 || (*sidecar.Args)[1] != "" {
		t.Fatalf("sidecar args = %v, want [--save \"\"]", sidecar.Args)
	}
	if sidecar.Ports == nil || (*sidecar.Ports)[0] != 6379 || sidecar.Cpu == nil || *sidecar.Cpu != "250m" || (*sidecar.Env)["MODE"] != "cache" {
		t.Fatalf("sidecar = %+v", sidecar)
	}
}

func TestPoolProcessAttributesOmittedWhenUnset(t *testing.T) {
	model := examplePoolModel()
	var diagnostics diag.Diagnostics
	vm := model.toSDKTemplateSpec(context.Background(), &diagnostics).VmTemplate
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if vm.Args != nil || vm.Env != nil || vm.ProcessMode != nil || vm.Sidecars != nil {
		t.Fatalf("args %v env %v processMode %v sidecars %v; want all omitted", vm.Args, vm.Env, vm.ProcessMode, vm.Sidecars)
	}
}

func TestPoolProcessRoundTripsThroughTheSDK(t *testing.T) {
	want := processPoolModel()
	var diagnostics diag.Diagnostics
	template := fleet_sdk.Template{Spec: want.toSDKTemplateSpec(context.Background(), &diagnostics)}

	var got poolResourceModel
	got.fromSDKTemplate(context.Background(), &template, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	for name, pair := range map[string][2]attr.Value{
		"args":         {got.Args, want.Args},
		"env":          {got.Env, want.Env},
		"process_mode": {got.ProcessMode, want.ProcessMode},
		"sidecar":      {got.Sidecars, want.Sidecars},
	} {
		if !pair[0].Equal(pair[1]) {
			t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
		}
	}
}

func TestPoolProcessOldShapedTemplateReadsAsUnset(t *testing.T) {
	template := fleet_sdk.Template{Spec: cyclops_sdk_schema.OsGymSandboxTemplateSpec{
		VmTemplate: cyclops_sdk_schema.VmTemplate{ContainerDiskImage: "registry.example/image:latest"},
	}}
	var model poolResourceModel
	var diagnostics diag.Diagnostics
	model.fromSDKTemplate(context.Background(), &template, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if !model.Args.IsNull() || !model.Env.IsNull() || !model.ProcessMode.IsNull() {
		t.Fatalf("args %v env %v processMode %v; want null", model.Args, model.Env, model.ProcessMode)
	}
	// No sidecar blocks plan as an empty list, so that is what an old
	// template must read as.
	if model.Sidecars.IsNull() || len(model.Sidecars.Elements()) != 0 {
		t.Fatalf("sidecars = %v, want an empty list", model.Sidecars)
	}
}

func TestPoolProcessUpdateRoutesToTheTemplate(t *testing.T) {
	state := examplePoolModel()
	changed := processPoolModel()
	for name, mutate := range map[string]func(*poolResourceModel){
		"args":         func(m *poolResourceModel) { m.Args = changed.Args },
		"env":          func(m *poolResourceModel) { m.Env = changed.Env },
		"process_mode": func(m *poolResourceModel) { m.ProcessMode = changed.ProcessMode },
		"sidecar":      func(m *poolResourceModel) { m.Sidecars = changed.Sidecars },
	} {
		model := state
		mutate(&model)
		if model.templateAttributesEqual(state) {
			t.Errorf("%s belongs to the template but did not register as changed", name)
		}
		if !model.warmPoolAttributesEqual(state) {
			t.Errorf("%s must not mark the warm pool as changed", name)
		}
	}
}
