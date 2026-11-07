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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestIPAMConversion(t *testing.T) {
	server := testWebhookServer(t)
	versions := []string{"v1alpha1", "v1beta1", "v1beta2"}
	for _, kind := range []string{"IPAddress", "IPAddressClaim"} {
		for _, sourceVersion := range versions {
			for _, targetVersion := range versions {
				if sourceVersion == targetVersion {
					continue
				}
				t.Run(kind+"/"+sourceVersion+"/"+targetVersion, func(t *testing.T) {
					obj := newIPAMObject(kind, sourceVersion)
					converted := convert(t, server.WebhookMux(), obj, "ipam.cluster.x-k8s.io/"+targetVersion)
					assertIPAMFields(t, converted, kind)
					roundTrip := convert(t, server.WebhookMux(), converted, obj.GetAPIVersion())
					assertIPAMFields(t, roundTrip, kind)
					if !reflect.DeepEqual(obj.Object["status"], roundTrip.Object["status"]) {
						t.Fatalf("status changed during round trip: got %+v, want %+v", roundTrip.Object["status"], obj.Object["status"])
					}
				})
			}
		}
	}
}

func newIPAMObject(kind, version string) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"poolRef": map[string]interface{}{"apiGroup": "ipam.example.com", "kind": "IPPool", "name": "pool"},
	}
	if kind == "IPAddress" {
		spec["address"] = "192.0.2.10"
		spec["prefix"] = int64(24)
		spec["gateway"] = "192.0.2.1"
		spec["claimRef"] = map[string]interface{}{"name": "claim"}
	}
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "ipam.cluster.x-k8s.io/" + version,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": "test", "namespace": "default", "uid": "object-uid"},
		"spec":       spec,
	}}
	if kind == "IPAddressClaim" {
		legacy := []interface{}{map[string]interface{}{
			"type": "Ready", "status": "True", "lastTransitionTime": "2026-01-01T00:00:00Z",
			"reason": "Allocated", "message": "Address allocated",
		}}
		modern := []interface{}{map[string]interface{}{
			"type": "Ready", "status": "True", "lastTransitionTime": "2026-01-01T00:00:00Z",
			"reason": "Allocated", "message": "Address allocated", "observedGeneration": int64(1),
		}}
		status := map[string]interface{}{"addressRef": map[string]interface{}{"name": "address"}}
		switch version {
		case "v1alpha1":
			status["conditions"] = legacy
		case "v1beta1":
			status["conditions"] = legacy
			status["v1beta2"] = map[string]interface{}{"conditions": modern}
		case "v1beta2":
			status["conditions"] = modern
			status["deprecated"] = map[string]interface{}{"v1beta1": map[string]interface{}{"conditions": legacy}}
		}
		obj.Object["status"] = status
	}
	return obj
}

func TestNonIPAMConversionRejected(t *testing.T) {
	server := testWebhookServer(t)
	for _, resource := range []struct {
		group, version, kind string
	}{
		{"cluster.x-k8s.io", "v1beta1", "Cluster"},
		{"addons.cluster.x-k8s.io", "v1beta1", "ClusterResourceSet"},
		{"runtime.cluster.x-k8s.io", "v1alpha1", "ExtensionConfig"},
	} {
		t.Run(resource.kind, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": resource.group + "/" + resource.version,
				"kind":       resource.kind,
				"metadata":   map[string]interface{}{"name": "test", "namespace": "default"},
			}}
			review := apiextensionsv1.ConversionReview{
				TypeMeta: metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "ConversionReview"},
				Request: &apiextensionsv1.ConversionRequest{
					UID:               "request-uid",
					DesiredAPIVersion: resource.group + "/v1beta2",
					Objects:           []runtime.RawExtension{{Object: obj}},
				},
			}
			body, err := json.Marshal(review)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			server.WebhookMux().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/convert", bytes.NewReader(body)))
			if err := json.Unmarshal(response.Body.Bytes(), &review); err != nil {
				t.Fatal(err)
			}
			if review.Response == nil || review.Response.Result.Status != metav1.StatusFailure || review.Response.UID != "request-uid" {
				t.Fatalf("expected non-IPAM conversion to be rejected: %s", response.Body.String())
			}
		})
	}
}

func TestInvalidTLSConfiguration(t *testing.T) {
	for _, opts := range []options{
		{tlsMinVersion: "invalid"},
		{tlsMinVersion: "VersionTLS12", tlsCipherSuites: []string{"invalid"}},
		{tlsCurvePreferences: []int32{-1}},
	} {
		if _, err := resolveTLSConfig(context.Background(), nil, opts); err == nil {
			t.Fatalf("expected invalid TLS configuration to be rejected: %+v", opts)
		}
	}
}

func TestWebhookRoutes(t *testing.T) {
	server := testWebhookServer(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.WebhookMux().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
			if response.Code != http.StatusOK {
				t.Fatalf("expected healthy endpoint, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
	for _, path := range []string{"/validate-ipam-cluster-x-k8s-io-v1beta2-ipaddress", "/mutate-ipam-cluster-x-k8s-io-v1beta2-ipaddressclaim"} {
		response := httptest.NewRecorder()
		server.WebhookMux().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, http.NoBody))
		if response.Code != http.StatusNotFound {
			t.Fatalf("unexpected admission webhook at %s: %d", path, response.Code)
		}
	}
	response := httptest.NewRecorder()
	server.WebhookMux().ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/convert", bytes.NewBufferString(`{}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid conversion request to be rejected, got %d", response.Code)
	}
}

func testWebhookServer(t *testing.T) webhook.Server {
	t.Helper()
	scheme, err := newScheme()
	if err != nil {
		t.Fatal(err)
	}
	opts := options{tlsMinVersion: "VersionTLS12"}
	tlsResult, err := resolveTLSConfig(context.Background(), nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	return newWebhookServer(scheme, opts, tlsResult.configureTLS)
}

func convert(t *testing.T, handler http.Handler, obj *unstructured.Unstructured, desiredAPIVersion string) *unstructured.Unstructured {
	t.Helper()
	review := apiextensionsv1.ConversionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "ConversionReview"},
		Request: &apiextensionsv1.ConversionRequest{
			UID:               "request-uid",
			DesiredAPIVersion: desiredAPIVersion,
			Objects:           []runtime.RawExtension{{Object: obj}},
		},
	}
	body, err := json.Marshal(review)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/convert", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("conversion returned HTTP %d: %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Response == nil || review.Response.Result.Status != metav1.StatusSuccess {
		t.Fatalf("conversion failed: %s", response.Body.String())
	}
	if review.Response.UID != "request-uid" || len(review.Response.ConvertedObjects) != 1 {
		t.Fatalf("unexpected conversion response: %+v", review.Response)
	}
	converted := &unstructured.Unstructured{}
	if err := json.Unmarshal(review.Response.ConvertedObjects[0].Raw, converted); err != nil {
		t.Fatal(err)
	}
	if converted.GetAPIVersion() != desiredAPIVersion || converted.GetKind() != obj.GetKind() {
		t.Fatalf("unexpected converted GVK: %s", converted.GroupVersionKind())
	}
	return converted
}

func assertIPAMFields(t *testing.T, obj *unstructured.Unstructured, kind string) {
	t.Helper()
	if obj.GetName() != "test" || obj.GetNamespace() != "default" || obj.GetUID() != "object-uid" {
		t.Fatalf("metadata changed during conversion: %+v", obj.Object["metadata"])
	}
	fields := map[string]string{"poolRef.apiGroup": "ipam.example.com", "poolRef.kind": "IPPool", "poolRef.name": "pool"}
	if kind == "IPAddress" {
		fields["address"] = "192.0.2.10"
		fields["gateway"] = "192.0.2.1"
		fields["claimRef.name"] = "claim"
		prefix, _, err := unstructured.NestedInt64(obj.Object, "spec", "prefix")
		if err != nil || prefix != 24 {
			t.Fatalf("prefix changed during conversion: %v, error=%v", prefix, err)
		}
	}
	for field, want := range fields {
		path := append([]string{"spec"}, strings.Split(field, ".")...)
		got, _, err := unstructured.NestedString(obj.Object, path...)
		if err != nil || got != want {
			t.Fatalf("%s changed during conversion: got %q, want %q, error=%v", field, got, want, err)
		}
	}
}
