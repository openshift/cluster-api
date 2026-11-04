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

// main is the main package for capi-ipam-conversion-webhook.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/component-base/logs"
	logsv1 "k8s.io/component-base/logs/api/v1"
	_ "k8s.io/component-base/logs/json/register"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	ipamv1alpha1 "sigs.k8s.io/cluster-api/api/ipam/v1alpha1"
	ipamv1beta1 "sigs.k8s.io/cluster-api/api/ipam/v1beta1"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"
)

type options struct {
	port                int
	certDir             string
	certName            string
	keyName             string
	tlsMinVersion       string
	tlsCipherSuites     []string
	tlsCurvePreferences []int32
}

func main() {
	opts := options{}
	logOptions := logs.NewOptions()
	logsv1.AddFlags(logOptions, pflag.CommandLine)
	pflag.IntVar(&opts.port, "webhook-port", 9443, "Port for the conversion webhook and HTTPS health endpoints.")
	pflag.StringVar(&opts.certDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/", "Directory containing the serving certificate and key.")
	pflag.StringVar(&opts.certName, "webhook-cert-name", "tls.crt", "Serving certificate file name within webhook-cert-dir.")
	pflag.StringVar(&opts.keyName, "webhook-key-name", "tls.key", "Serving key file name within webhook-cert-dir.")
	pflag.StringVar(&opts.tlsMinVersion, "tls-min-version", "", "Minimum TLS version; when set, overrides the cluster-wide TLS profile.")
	pflag.StringSliceVar(&opts.tlsCipherSuites, "tls-cipher-suites", nil, "Comma-separated TLS cipher suites; when set, overrides the cluster-wide TLS profile.")
	pflag.Int32SliceVar(&opts.tlsCurvePreferences, "tls-curve-preferences", nil, "Comma-separated numeric Go crypto/tls CurveID values; when set, overrides the cluster-wide TLS profile.")
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if err := logsv1.ValidateAndApply(logOptions, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid logging configuration: %v\n", err)
		os.Exit(1)
	}
	ctrl.SetLogger(klog.Background())
	if err := run(ctrl.SetupSignalHandler(), opts); err != nil {
		ctrl.Log.Error(err, "Unable to run conversion webhook")
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	scheme, err := newScheme()
	if err != nil {
		return fmt.Errorf("create IPAM conversion scheme: %w", err)
	}
	if opts.hasTLSOverrides() {
		tlsResult, err := resolveTLSConfig(ctx, nil, opts)
		if err != nil {
			return fmt.Errorf("resolve TLS overrides: %w", err)
		}
		if err := newWebhookServer(scheme, opts, tlsResult.configureTLS).Start(ctx); err != nil {
			return fmt.Errorf("start conversion webhook: %w", err)
		}
		return nil
	}

	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("load Kubernetes configuration for the TLS profile: %w", err)
	}
	configScheme := runtime.NewScheme()
	if err := configv1.AddToScheme(configScheme); err != nil {
		return fmt.Errorf("register OpenShift configuration types: %w", err)
	}
	c, err := client.New(config, client.Options{Scheme: configScheme})
	if err != nil {
		return fmt.Errorf("create TLS profile client: %w", err)
	}
	startupCtx, startupCancel := context.WithTimeout(ctx, 30*time.Second)
	tlsResult, err := resolveTLSConfig(startupCtx, c, opts)
	startupCancel()
	if err != nil {
		return fmt.Errorf("resolve cluster TLS configuration: %w", err)
	}
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 configScheme,
		Metrics:                server.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		WebhookServer:          newWebhookServer(scheme, opts, tlsResult.configureTLS),
	})
	if err != nil {
		return fmt.Errorf("create webhook manager: %w", err)
	}
	// The manager adds its webhook server as a runnable only when it is accessed.
	mgr.GetWebhookServer()
	if err := newTLSProfileWatcher(mgr.GetClient(), tlsResult, cancel).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("set up TLS profile watcher: %w", err)
	}
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("start webhook manager: %w", err)
	}
	return nil
}

func newScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	builder := runtime.NewSchemeBuilder(
		// Non-IPAM conversions are disabled. Re-enabling core conversions also
		// requires configuring the API version getter and a CRD metadata client.
		// clusterv1beta1.AddToScheme, clusterv1.AddToScheme,
		// addonsv1beta1.AddToScheme, addonsv1.AddToScheme,
		// runtimev1alpha1.AddToScheme, runtimev1.AddToScheme,
		ipamv1alpha1.AddToScheme, ipamv1beta1.AddToScheme, ipamv1.AddToScheme,
	)
	if err := builder.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("register IPAM API versions: %w", err)
	}
	return scheme, nil
}

func newWebhookServer(scheme *runtime.Scheme, opts options, configureTLS func(*tls.Config)) webhook.Server {
	server := webhook.NewServer(webhook.Options{
		Port:     opts.port,
		CertDir:  opts.certDir,
		CertName: opts.certName,
		KeyName:  opts.keyName,
		TLSOpts:  []func(*tls.Config){configureTLS},
	})
	server.Register("/convert", conversion.NewWebhookHandler(scheme, conversion.NewRegistry()))
	// These endpoints are served on the same TLS listener as conversion, so a
	// successful probe also verifies that the serving certificate has been loaded.
	server.Register("/healthz", healthz.CheckHandler{Checker: healthz.Ping})
	server.Register("/readyz", healthz.CheckHandler{Checker: healthz.Ping})
	return server
}
