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
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	utiltls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type tlsConfigResult struct {
	configureTLS func(*tls.Config)
	adherence    configv1.TLSAdherencePolicy
	profile      configv1.TLSProfileSpec
}

func (opts options) hasTLSOverrides() bool {
	return opts.tlsMinVersion != "" || len(opts.tlsCipherSuites) > 0 || len(opts.tlsCurvePreferences) > 0
}

func resolveTLSConfig(ctx context.Context, c client.Client, opts options) (tlsConfigResult, error) {
	if opts.hasTLSOverrides() {
		minVersion, err := cliflag.TLSVersion(opts.tlsMinVersion)
		if err != nil {
			return tlsConfigResult{}, fmt.Errorf("invalid --tls-min-version: %w", err)
		}
		cipherSuites, err := cliflag.TLSCipherSuites(opts.tlsCipherSuites)
		if err != nil {
			return tlsConfigResult{}, fmt.Errorf("invalid --tls-cipher-suites: %w", err)
		}
		curvePreferences, err := cliflag.TLSCurvePreferences(opts.tlsCurvePreferences)
		if err != nil {
			return tlsConfigResult{}, fmt.Errorf("invalid --tls-curve-preferences: %w", err)
		}
		klog.Info("TLS configuration overridden via CLI flags; cluster TLS profile watcher disabled")
		if minVersion == tls.VersionTLS13 && len(opts.tlsCipherSuites) > 0 {
			klog.Warning("TLS 1.3 cipher suites are not configurable in Go; ignoring --tls-cipher-suites")
		}
		return tlsConfigResult{configureTLS: func(config *tls.Config) {
			config.MinVersion = minVersion
			if len(curvePreferences) > 0 {
				config.CurvePreferences = curvePreferences
			}
			if minVersion != tls.VersionTLS13 {
				config.CipherSuites = cipherSuites
			}
		}}, nil
	}

	adherence, err := utiltls.FetchAPIServerTLSAdherencePolicy(ctx, c)
	if err != nil {
		klog.Errorf("Unable to fetch TLS adherence policy; using the default policy: %v", err)
		adherence = configv1.TLSAdherencePolicyNoOpinion
	}
	profile, err := utiltls.FetchAPIServerTLSProfile(ctx, c)
	if err != nil {
		klog.Errorf("Unable to fetch TLS profile; using the default profile: %v", err)
		profile = *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	}
	activeProfile := profile
	if !libgocrypto.ShouldHonorClusterTLSProfile(adherence) {
		activeProfile = *configv1.TLSProfiles[libgocrypto.DefaultTLSProfileType]
	}
	configureTLS, unsupported := utiltls.NewTLSConfigFromProfile(activeProfile)
	if len(unsupported) > 0 {
		klog.Warningf("TLS profile contains unsupported ciphers/groups that will be ignored: %v", unsupported)
	}
	return tlsConfigResult{configureTLS: configureTLS, adherence: adherence, profile: profile}, nil
}

func newTLSProfileWatcher(c client.Client, initial tlsConfigResult, cancel context.CancelFunc) *utiltls.SecurityProfileWatcher {
	return &utiltls.SecurityProfileWatcher{
		Client:                    c,
		InitialTLSAdherencePolicy: initial.adherence,
		InitialTLSProfileSpec:     initial.profile,
		OnProfileChange: func(_ context.Context, _, _ configv1.TLSProfileSpec) {
			klog.Info("TLS profile has changed, initiating a shutdown to reload it")
			cancel()
		},
		OnAdherencePolicyChange: func(_ context.Context, _, _ configv1.TLSAdherencePolicy) {
			klog.Info("TLS adherence policy has changed, initiating a shutdown to reload it")
			cancel()
		},
	}
}
