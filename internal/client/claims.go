package client

import (
	"context"
	"net/http"
)

const (
	claimAPIGroup     = "osgym.cua.ai"
	claimAPIVersion   = "v1alpha1"
	claimResource     = "osgymsandboxclaims"
	claimResourceKind = "OSGymSandboxClaim"
)

// Claim is the API representation of an OSGymSandboxClaim. A claim binds one
// sandbox from the pool whose namespace it belongs to.
type Claim struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Metadata   ClaimMetadata `json:"metadata"`
	Spec       ClaimSpec     `json:"spec"`
	Status     ClaimStatus   `json:"status,omitempty"`
}

type ClaimMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

type ClaimSpec struct {
	SandboxTemplateRef SandboxTemplateRef `json:"sandboxTemplateRef"`
}

type SandboxTemplateRef struct {
	Name string `json:"name"`
}

type ClaimStatus struct {
	Phase      string       `json:"phase,omitempty"`
	Sandbox    ClaimSandbox `json:"sandbox,omitempty"`
	Conditions []Condition  `json:"conditions,omitempty"`
}

type ClaimSandbox struct {
	Name    string `json:"name,omitempty"`
	Service string `json:"service,omitempty"`
}

type Condition struct {
	Type    string `json:"type,omitempty"`
	Status  string `json:"status,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

func (c *Client) CreateClaim(ctx context.Context, poolName, claimName string) (Claim, error) {
	claim := Claim{
		APIVersion: claimAPIGroup + "/" + claimAPIVersion,
		Kind:       claimResourceKind,
		Metadata:   ClaimMetadata{Name: claimName},
		Spec:       ClaimSpec{SandboxTemplateRef: SandboxTemplateRef{Name: poolName + "-template"}},
	}
	err := c.do(ctx, http.MethodPost, claimCollectionPath(poolName), "", claim, &claim)
	return claim, err
}

func (c *Client) GetClaim(ctx context.Context, poolName, claimName string) (Claim, error) {
	var claim Claim
	err := c.do(ctx, http.MethodGet, claimPath(poolName, claimName), "", nil, &claim)
	return claim, err
}

func (c *Client) DeleteClaim(ctx context.Context, poolName, claimName string) error {
	if err := c.do(ctx, http.MethodDelete, claimPath(poolName, claimName), "", nil, nil); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

func claimCollectionPath(poolName string) string {
	return "/api/k8s/apis/" + claimAPIGroup + "/" + claimAPIVersion + "/namespaces/" + escape(poolName) + "/" + claimResource
}

func claimPath(poolName, claimName string) string {
	return claimCollectionPath(poolName) + "/" + escape(claimName)
}
