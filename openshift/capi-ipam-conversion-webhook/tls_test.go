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
	"reflect"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	utiltls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveTLSConfigFlags(t *testing.T) {
	for _, tt := range []struct {
		name    string
		opts    options
		version uint16
		ciphers []uint16
		curves  []tls.CurveID
	}{
		{
			name:    "all flags",
			opts:    options{tlsMinVersion: "VersionTLS12", tlsCipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"}, tlsCurvePreferences: []int32{29, 23}},
			version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}, curves: []tls.CurveID{tls.X25519, tls.CurveP256},
		},
		{
			name:    "curves only",
			opts:    options{tlsCurvePreferences: []int32{29, 23}},
			version: tls.VersionTLS12, curves: []tls.CurveID{tls.X25519, tls.CurveP256},
		},
		{
			name:    "ciphers only",
			opts:    options{tlsCipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"}},
			version: tls.VersionTLS12, ciphers: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		},
		{
			name:    "version only",
			opts:    options{tlsMinVersion: "VersionTLS12"},
			version: tls.VersionTLS12,
		},
		{
			name:    "TLS13 preserves cipher suites",
			opts:    options{tlsMinVersion: "VersionTLS13", tlsCipherSuites: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"}, tlsCurvePreferences: []int32{29, 23}},
			version: tls.VersionTLS13, ciphers: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384}, curves: []tls.CurveID{tls.X25519, tls.CurveP256},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.opts.hasTLSOverrides() {
				t.Fatal("flags must enable TLS overrides")
			}
			// A nil client proves overrides do not fetch cluster configuration.
			result, err := resolveTLSConfig(context.Background(), nil, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			config := &tls.Config{CipherSuites: []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384}}
			result.configureTLS(config)
			assertTLSSettings(t, config, &tls.Config{MinVersion: tt.version, CipherSuites: tt.ciphers, CurvePreferences: tt.curves})
		})
	}
}

func TestResolveTLSConfigCluster(t *testing.T) {
	if (options{}).hasTLSOverrides() {
		t.Fatal("empty options must not enable TLS overrides")
	}
	custom := testTLSAPIServer()
	noOpinion := custom.DeepCopy()
	noOpinion.Spec.TLSAdherence = configv1.TLSAdherencePolicyNoOpinion
	for _, tt := range []struct {
		name   string
		server *configv1.APIServer
	}{
		{name: "strict custom profile", server: custom},
		{name: "NoOpinion uses shared default", server: noOpinion},
		{name: "missing APIServer uses shared default"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := testTLSClient(t, tt.server)
			result, err := resolveTLSConfig(context.Background(), c, options{})
			if err != nil {
				t.Fatal(err)
			}
			config := &tls.Config{}
			result.configureTLS(config)
			wantAdherence := configv1.TLSAdherencePolicyNoOpinion
			wantProfile := *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
			if tt.server != nil {
				wantAdherence = tt.server.Spec.TLSAdherence
				wantProfile = tt.server.Spec.TLSSecurityProfile.Custom.TLSProfileSpec
			}
			if result.adherence != wantAdherence || !reflect.DeepEqual(result.profile, wantProfile) {
				t.Fatalf("initial state = (%q, %+v), want (%q, %+v)", result.adherence, result.profile, wantAdherence, wantProfile)
			}
			want := &tls.Config{}
			if tt.server == custom {
				want.MinVersion = tls.VersionTLS12
				want.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}
				want.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256}
			} else {
				configure, unsupported := utiltls.NewTLSConfigFromProfile(*configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType])
				if len(unsupported) != 0 {
					t.Fatalf("unsupported default settings: %v", unsupported)
				}
				configure(want)
			}
			assertTLSSettings(t, config, want)
		})
	}
}

func TestTLSProfileWatcher(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*configv1.APIServer)
	}{
		{name: "unchanged"},
		{name: "groups changed", change: func(server *configv1.APIServer) {
			server.Spec.TLSSecurityProfile.Custom.Groups = []configv1.TLSGroup{configv1.TLSGroupSecP256r1}
		}},
		{name: "adherence changed", change: func(server *configv1.APIServer) {
			server.Spec.TLSAdherence = configv1.TLSAdherencePolicyNoOpinion
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			server := testTLSAPIServer()
			c := testTLSClient(t, server)
			initial, err := resolveTLSConfig(ctx, c, options{})
			if err != nil {
				t.Fatal(err)
			}
			shutdownCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			watcher := newTLSProfileWatcher(c, initial, cancel)
			request := ctrl.Request{NamespacedName: types.NamespacedName{Name: server.Name}}
			if _, err := watcher.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if shutdownCtx.Err() != nil {
				t.Fatal("unchanged initial configuration cancelled context")
			}
			if tt.change != nil {
				if err := c.Get(ctx, request.NamespacedName, server); err != nil {
					t.Fatal(err)
				}
				tt.change(server)
				if err := c.Update(ctx, server); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := watcher.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
			if got := shutdownCtx.Err() != nil; got != (tt.change != nil) {
				t.Fatalf("cancelled = %t, want %t", got, tt.change != nil)
			}
		})
	}
}

func testTLSAPIServer() *configv1.APIServer {
	return &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configv1.APIServerSpec{
			TLSAdherence: configv1.TLSAdherencePolicyStrictAllComponents,
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: configv1.TLSProfileSpec{
					MinTLSVersion: configv1.VersionTLS12,
					Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
					Groups:        []configv1.TLSGroup{configv1.TLSGroupX25519, configv1.TLSGroupSecP256r1},
				}},
			},
		},
	}
}

func testTLSClient(t *testing.T, server *configv1.APIServer) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := configv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	if server != nil {
		builder.WithObjects(server)
	}
	return builder.Build()
}

func assertTLSSettings(t *testing.T, got, want *tls.Config) {
	t.Helper()
	if got.MinVersion != want.MinVersion || !reflect.DeepEqual(got.CipherSuites, want.CipherSuites) || !reflect.DeepEqual(got.CurvePreferences, want.CurvePreferences) {
		t.Fatalf("TLS settings = (%#x, %v, %v), want (%#x, %v, %v)", got.MinVersion, got.CipherSuites, got.CurvePreferences, want.MinVersion, want.CipherSuites, want.CurvePreferences)
	}
}
