//! Absolute size backstops in the generated CRDs. The per-account limits
//! (1-8 vCPU, 512Mi-32Gi) live in cyclops-cs/backend/auth/pool_admission.rego;
//! these maxima only stop requests no node could ever schedule, and sit above
//! every object that was live when they were added, so no existing object
//! fails validation on its next update.

use cyclops_sdk_schema::{OSGymSandbox, OSGymSandboxTemplate, OSGymSandboxWarmPool};
use kube::CustomResourceExt;
use serde_json::{Value, json};

const VM_TEMPLATE: &str =
    "/spec/versions/0/schema/openAPIV3Schema/properties/spec/properties/vmTemplate/properties";
const WARM_POOL_SPEC: &str = "/spec/versions/0/schema/openAPIV3Schema/properties/spec/properties";
const MEMORY_PATTERN: &str = r"^[0-9]+(\.[0-9]+)?(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?$";

fn vm_template_properties(crd: Value) -> Value {
    crd.pointer(VM_TEMPLATE).unwrap().clone()
}

#[test]
fn vm_template_cpu_cores_are_bounded() {
    for crd in [
        serde_json::to_value(OSGymSandbox::crd()).unwrap(),
        serde_json::to_value(OSGymSandboxTemplate::crd()).unwrap(),
    ] {
        let properties = vm_template_properties(crd);
        assert_eq!(properties.pointer("/cpuCores/minimum"), Some(&json!(1.0)));
        assert_eq!(properties.pointer("/cpuCores/maximum"), Some(&json!(64.0)));
        assert_eq!(properties.pointer("/cpuCores/default"), Some(&json!(4)));
    }
}

#[test]
fn vm_template_memory_must_be_a_quantity() {
    for crd in [
        serde_json::to_value(OSGymSandbox::crd()).unwrap(),
        serde_json::to_value(OSGymSandboxTemplate::crd()).unwrap(),
    ] {
        let properties = vm_template_properties(crd);
        assert_eq!(properties.pointer("/memory/type"), Some(&json!("string")));
        assert_eq!(
            properties.pointer("/memory/pattern"),
            Some(&json!(MEMORY_PATTERN))
        );
        assert_eq!(properties.pointer("/memory/default"), Some(&json!("4Gi")));
    }
}

// pool_admission.rego tests the pattern against good and bad quantities; this
// keeps the schema and the policy on the same one.
#[test]
fn memory_pattern_matches_pool_admission() {
    let policy = include_str!("../../backend/auth/pool_admission.rego");
    assert!(
        policy.contains(&format!("memory_quantity_pattern := `{MEMORY_PATTERN}`")),
        "pool_admission.rego memory_quantity_pattern differs from the CRD pattern"
    );
}

#[test]
fn warm_pool_sizes_are_bounded() {
    let crd = serde_json::to_value(OSGymSandboxWarmPool::crd()).unwrap();
    let spec = crd.pointer(WARM_POOL_SPEC).unwrap();

    // KEDA writes spec.replicas through /scale up to maxPoolSize, so the two
    // share one ceiling: a lower replicas maximum would fail its scale-ups.
    assert_eq!(spec.pointer("/replicas/maximum"), Some(&json!(50.0)));
    for field in ["maxPoolSize", "minPoolSize", "initialPoolSize"] {
        assert_eq!(
            spec.pointer(&format!("/autoscaling/properties/{field}/maximum")),
            Some(&json!(50.0)),
            "{field}"
        );
    }
    assert_eq!(
        spec.pointer("/autoscaling/properties/maxPoolSize/default"),
        Some(&json!(50))
    );
}
