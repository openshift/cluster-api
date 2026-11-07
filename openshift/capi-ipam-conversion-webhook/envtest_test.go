/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestIPAMEnvtest exercises the production startup path and API-server-driven
// conversion using the standalone payload's CRDs and envtest's serving certificate.
func TestIPAMEnvtest(t *testing.T) {
	c, webhookOptions := setupIPAMEnvtest(t)
	done := startEnvtestWebhook(t, webhookOptions)
	ctx := t.Context()

	for _, kind := range []string{"IPAddress", "IPAddressClaim"} {
		for _, source := range []string{"v1alpha1", "v1beta1", "v1beta2"} {
			for _, target := range []string{"v1alpha1", "v1beta1", "v1beta2"} {
				if source == target {
					continue
				}
				t.Run(kind+"/"+source+"/"+target, func(t *testing.T) {
					original := newIPAMObject(kind, source)
					original.SetUID("")
					original.SetNamespace("ipam-conversion-test")
					status := original.Object["status"]
					delete(original.Object, "status")
					if err := c.Create(ctx, original); err != nil {
						t.Fatalf("create source object: %v", err)
					}
					t.Cleanup(func() {
						if err := c.Delete(ctx, original); err != nil {
							t.Errorf("delete source object: %v", err)
						}
					})
					if kind == "IPAddressClaim" {
						// Kubernetes ignores status on creation; populate it through the
						// status subresource before testing cross-version preservation.
						original.Object["status"] = status
						if err := c.Status().Update(ctx, original); err != nil {
							t.Fatalf("populate source claim status: %v", err)
						}
					}
					converted := &unstructured.Unstructured{}
					converted.SetAPIVersion("ipam.cluster.x-k8s.io/" + target)
					converted.SetKind(kind)
					key := client.ObjectKeyFromObject(original)
					if err := c.Get(ctx, key, converted); err != nil {
						t.Fatalf("read object in target version: %v", err)
					}
					if !reflect.DeepEqual(original.Object["spec"], converted.Object["spec"]) {
						t.Fatalf("spec changed when reading the target version: got %+v, want %+v", converted.Object["spec"], original.Object["spec"])
					}
					list := &unstructured.UnstructuredList{}
					list.SetAPIVersion(converted.GetAPIVersion())
					list.SetKind(kind + "List")
					if err := c.List(ctx, list, client.InNamespace(original.GetNamespace())); err != nil {
						t.Fatalf("list objects in target version: %v", err)
					}
					if len(list.Items) != 1 || list.Items[0].GetUID() != original.GetUID() {
						t.Fatalf("unexpected converted list: %+v", list.Items)
					}
					// Write through the target API and read back through the source API.
					// Preserve annotations: older versions carry conversion data there.
					converted.SetLabels(map[string]string{"conversion-test": target})
					if err := c.Update(ctx, converted); err != nil {
						t.Fatalf("update object in target version: %v", err)
					}
					roundTrip := &unstructured.Unstructured{}
					roundTrip.SetGroupVersionKind(original.GroupVersionKind())
					if err := c.Get(ctx, key, roundTrip); err != nil {
						t.Fatalf("read object back in source version: %v", err)
					}
					if roundTrip.GetUID() != original.GetUID() || roundTrip.GetLabels()["conversion-test"] != target {
						t.Fatalf("metadata changed during round trip: %+v", roundTrip.Object["metadata"])
					}
					for _, field := range []string{"spec", "status"} {
						if !reflect.DeepEqual(original.Object[field], roundTrip.Object[field]) {
							t.Fatalf("%s changed during API-server round trip: got %+v, want %+v", field, roundTrip.Object[field], original.Object[field])
						}
					}
				})
			}
		}
	}
	apiServer := &configv1.APIServer{}
	// A real watch event must stop the production process when its TLS profile changes.
	if err := c.Get(ctx, types.NamespacedName{Name: "cluster"}, apiServer); err != nil {
		t.Fatalf("read cluster TLS configuration before profile change: %v", err)
	}
	apiServer.Spec.TLSSecurityProfile.Custom.MinTLSVersion = configv1.VersionTLS13
	if err := c.Update(ctx, apiServer); err != nil {
		t.Fatalf("update cluster TLS profile: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TLS profile change: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("TLS profile change did not stop the webhook")
	}
}

// setupIPAMEnvtest installs the payload CRDs and TLS configuration and points
// the production kubeconfig lookup at the local API server.
func setupIPAMEnvtest(t *testing.T) (client.Client, envtest.WebhookInstallOptions) {
	t.Helper()
	scheme, err := newScheme()
	if err != nil {
		t.Fatal(err)
	}
	if err := configv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	// Envtest has no OpenShift config API. Persist TLS settings in a minimal CRD
	// so the production client and watcher use a real API server, not a mock.
	configCRD := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "apiservers.config.openshift.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "config.openshift.io",
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "apiservers", Singular: "apiserver", Kind: "APIServer", ListKind: "APIServerList",
			},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {Type: "object", XPreserveUnknownFields: ptr.To(true)},
					},
				}},
			}},
		},
	}
	// Install the shipped IPAM schemas and storage settings. Envtest replaces
	// their Service references with a local HTTPS URL and supplies its CA bundle.
	environment := &envtest.Environment{
		Scheme:                scheme,
		CRDDirectoryPaths:     []string{filepath.Join("..", "manifests", "0000_30_cluster-api_04_crd.core-cluster-api.yaml")},
		CRDs:                  []*apiextensionsv1.CustomResourceDefinition{configCRD},
		ErrorIfCRDPathMissing: true,
	}
	config, err := environment.Start()
	if err != nil {
		t.Fatalf("start envtest control plane and install CRDs: %v", err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Errorf("stop envtest control plane: %v", err)
		}
	})
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	apiServer := testTLSAPIServer()
	// Envtest generates an ECDSA serving certificate.
	apiServer.Spec.TLSSecurityProfile.Custom.Ciphers = []string{"ECDHE-ECDSA-AES128-GCM-SHA256"}
	if err := c.Create(ctx, apiServer); err != nil {
		t.Fatalf("create cluster TLS configuration: %v", err)
	}
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ipam-conversion-test"}}); err != nil {
		t.Fatalf("create IPAM test namespace: %v", err)
	}
	// Let run() discover the test API server through its normal kubeconfig path,
	// with TLS settings fetched from the API rather than overridden by flags.
	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	if err := clientcmd.WriteToFile(clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"envtest": {
			Server: config.Host, CertificateAuthorityData: config.CAData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"envtest": {
			ClientCertificateData: config.CertData, ClientKeyData: config.KeyData,
		}},
		Contexts:       map[string]*clientcmdapi.Context{"envtest": {Cluster: "envtest", AuthInfo: "envtest"}},
		CurrentContext: "envtest",
	}, kubeconfig); err != nil {
		t.Fatalf("write envtest kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfig)
	return c, environment.WebhookInstallOptions
}

// startEnvtestWebhook runs the production startup path and waits for HTTPS health
// checks. The returned channel reports shutdown after a TLS profile change.
func startEnvtestWebhook(t *testing.T, webhookOptions envtest.WebhookInstallOptions) <-chan error {
	t.Helper()
	ctx := t.Context()
	address := "https://" + net.JoinHostPort(webhookOptions.LocalServingHost, fmt.Sprint(webhookOptions.LocalServingPort))
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(webhookOptions.LocalServingCAData) {
		t.Fatal("invalid envtest serving CA")
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- run(runCtx, options{port: webhookOptions.LocalServingPort, certDir: webhookOptions.LocalServingCertDir})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop conversion webhook: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("webhook did not shut down")
		}
	})
	// Trust the serving CA and use TLS 1.2 to exercise the configured cipher
	// suite; TLS 1.3 would negotiate its own cipher suites instead.
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: time.Second}
	for _, path := range []string{"/healthz", "/readyz"} {
		if err := wait.PollUntilContextTimeout(ctx, 20*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+path, http.NoBody)
			if err != nil {
				return false, err
			}
			response, err := httpClient.Do(request)
			if err != nil {
				// The listener and serving certificate become available asynchronously.
				return false, nil
			}
			defer response.Body.Close()
			return response.StatusCode == http.StatusOK, nil
		}); err != nil {
			t.Fatalf("webhook %s did not become healthy with the cluster TLS profile: %v", path, err)
		}
	}
	return done
}
