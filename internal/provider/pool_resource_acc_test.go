//go:build acceptance

package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	fleetsprovider "github.com/trycua/terraform-provider-fleets/internal/provider"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var poolGVR = schema.GroupVersionResource{Group: "cua.ai", Version: "v1", Resource: "osgymworkspacepools"}
var claimGVR = schema.GroupVersionResource{Group: "osgym.cua.ai", Version: "v1alpha1", Resource: "osgymsandboxclaims"}

func TestAccPoolLifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC must be set for acceptance tests")
	}
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS must point to envtest binaries")
	}
	crdDirectory := os.Getenv("FLEETS_CRD_DIR")
	if crdDirectory == "" {
		t.Skip("FLEETS_CRD_DIR must point to the directory containing the Fleet CRDs")
	}

	testEnvironment := &envtest.Environment{
		CRDDirectoryPaths:     []string{crdDirectory},
		ErrorIfCRDPathMissing: true,
	}
	config, err := testEnvironment.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		if err := testEnvironment.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	apiServer := newCyclopsTestServer(t, clientset, dynamicClient)
	defer apiServer.Close()

	providerConfig := fmt.Sprintf(`
provider "fleets" {
  endpoint      = %q
  client_id     = "terraform-e2e"
  client_secret = "terraform-secret"
  token_url     = %q
}
`, apiServer.URL, apiServer.URL+"/token")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"fleets": providerserver.NewProtocol6WithError(fleetsprovider.New("test")()),
		},
		CheckDestroy: func(_ *terraform.State) error {
			_, err := dynamicClient.Resource(poolGVR).Namespace("terraform-e2e").Get(context.Background(), "terraform-e2e", metav1.GetOptions{})
			if err == nil {
				return fmt.Errorf("pool still exists after destroy")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: providerConfig + poolConfig(1, "4Gi") + claimConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fleets_pool.test", "name", "terraform-e2e"),
					resource.TestCheckResourceAttr("fleets_pool.test", "namespace", "terraform-e2e"),
					resource.TestCheckResourceAttr("fleets_pool.test", "replicas", "1"),
					resource.TestCheckResourceAttr("fleets_pool.test", "service.#", "1"),
					resource.TestCheckResourceAttr("fleets_pool.test", "autoscaling.max_pool_size", "5"),
					resource.TestCheckResourceAttr("fleets_claim.test", "phase", "Bound"),
					resource.TestCheckResourceAttr("fleets_claim.test", "sandbox_name", "terraform-claim-sandbox"),
					resource.TestCheckResourceAttr("fleets_claim.test", "sandbox_service", "terraform-claim-sandbox.terraform-e2e.svc.cluster.local"),
				),
			},
			{
				Config: providerConfig + poolConfig(2, "8Gi") + claimConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("fleets_pool.test", "replicas", "2"),
					resource.TestCheckResourceAttr("fleets_pool.test", "memory", "8Gi"),
				),
			},
			{
				ResourceName:      "fleets_pool.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				ResourceName:      "fleets_claim.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateId:     "terraform-e2e/terraform-claim",
			},
		},
	})
}

func poolConfig(replicas int, memory string) string {
	return fmt.Sprintf(`
resource "fleets_pool" "test" {
  name                 = "terraform-e2e"
  replicas             = %d
  cpu_cores            = 2
  memory               = %q
  container_disk_image = "example.invalid/cyclops/e2e:latest"

  autoscaling {
    min_pool_size     = 0
    initial_pool_size = 1
    max_pool_size     = 5
  }

  service {
    name        = "ssh"
    target_port = 22
    protocol    = "TCP"
  }
}
`, replicas, memory)
}

func claimConfig() string {
	return `
resource "fleets_claim" "test" {
  pool_name = fleets_pool.test.name
  claim_name = "terraform-claim"
}
`
}

func newCyclopsTestServer(t *testing.T, clientset *kubernetes.Clientset, dynamicClient dynamic.Interface) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			clientID, secret, ok := r.BasicAuth()
			if !ok || clientID != "terraform-e2e" || secret != "terraform-secret" {
				http.Error(w, "invalid client credentials", http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"access_token": "terraform-e2e-token", "expires_in": 300})
			return
		}
		if r.Header.Get("Authorization") != "Bearer terraform-e2e-token" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}

		ctx := r.Context()
		if r.URL.Path == "/api/namespaces" && r.Method == http.MethodPost {
			var request struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_, err := clientset.CoreV1().Namespaces().Create(ctx, namespaceObject(request.Name), metav1.CreateOptions{})
			writeKubernetesResult(w, http.StatusCreated, nil, err)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/namespaces/") && r.Method == http.MethodDelete {
			name := strings.TrimPrefix(r.URL.Path, "/api/namespaces/")
			err := clientset.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
			writeKubernetesResult(w, http.StatusNoContent, nil, err)
			return
		}

		const poolPrefix = "/api/k8s/apis/cua.ai/v1/namespaces/"
		const claimPrefix = "/api/k8s/apis/osgym.cua.ai/v1alpha1/namespaces/"
		var resourceGVR schema.GroupVersionResource
		var resourceName string
		var claimResource bool
		path := ""
		switch {
		case strings.HasPrefix(r.URL.Path, poolPrefix):
			path = strings.TrimPrefix(r.URL.Path, poolPrefix)
			resourceGVR = poolGVR
			resourceName = "osgymworkspacepools"
		case strings.HasPrefix(r.URL.Path, claimPrefix):
			path = strings.TrimPrefix(r.URL.Path, claimPrefix)
			resourceGVR = claimGVR
			resourceName = "osgymsandboxclaims"
			claimResource = true
		default:
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(path, "/")
		if len(parts) < 2 || parts[1] != resourceName {
			http.NotFound(w, r)
			return
		}
		namespace := parts[0]
		resources := dynamicClient.Resource(resourceGVR).Namespace(namespace)
		switch {
		case r.Method == http.MethodPost && len(parts) == 2:
			var object unstructured.Unstructured
			if err := json.NewDecoder(r.Body).Decode(&object.Object); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			created, err := resources.Create(ctx, &object, metav1.CreateOptions{})
			writeKubernetesResult(w, http.StatusCreated, created, err)
		case r.Method == http.MethodGet && len(parts) == 3:
			object, err := resources.Get(ctx, parts[2], metav1.GetOptions{})
			if err == nil && claimResource {
				object.Object["status"] = map[string]any{
					"phase": "Bound",
					"sandbox": map[string]any{
						"name":    parts[2] + "-sandbox",
						"service": parts[2] + "-sandbox." + namespace + ".svc.cluster.local",
					},
				}
			}
			writeKubernetesResult(w, http.StatusOK, object, err)
		case r.Method == http.MethodPatch && len(parts) == 3 && !claimResource:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			object, err := resources.Patch(ctx, parts[2], types.MergePatchType, body, metav1.PatchOptions{})
			writeKubernetesResult(w, http.StatusOK, object, err)
		case r.Method == http.MethodDelete && len(parts) == 3:
			if !claimResource {
				claims, err := dynamicClient.Resource(claimGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
				if err != nil {
					writeKubernetesResult(w, http.StatusInternalServerError, nil, err)
					return
				}
				if len(claims.Items) != 0 {
					writeJSON(w, http.StatusConflict, map[string]string{"error": "claims must be released before deleting a pool"})
					return
				}
			}
			err := resources.Delete(ctx, parts[2], metav1.DeleteOptions{})
			writeKubernetesResult(w, http.StatusNoContent, nil, err)
		default:
			http.NotFound(w, r)
		}
	}))
}

func namespaceObject(name string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func writeKubernetesResult(w http.ResponseWriter, successStatus int, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if apierrors.IsNotFound(err) {
			status = http.StatusNotFound
		} else if apierrors.IsAlreadyExists(err) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, successStatus, value)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
