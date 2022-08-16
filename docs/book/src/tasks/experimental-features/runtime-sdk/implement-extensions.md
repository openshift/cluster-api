# Implementing Runtime Extensions

<aside class="note warning">

<h1>Caution</h1>

Please note Runtime SDK is an advanced feature. If implemented incorrectly, a failing Runtime Extension can severely impact the Cluster API runtime.

</aside>

## Introduction

As a developer building systems on top of Cluster API, if you want to hook into the Cluster’s lifecycle via
a Runtime Hook, you have to implement a Runtime Extension handling requests according to the
OpenAPI specification for the Runtime Hook you are interested in.

Runtime Extensions by design are very powerful and flexible, however given that with great power comes
great responsibility, a few key consideration should always be kept in mind (more details in the following sections):

- Runtime Extensions are components that should be designed, written and deployed with great caution given that they
  can affect the proper functioning of the Cluster API runtime.
- Cluster administrators should carefully vet any Runtime Extension registration, thus preventing malicious components
  from being added to the system.

Please note that following similar practices is already commonly accepted in the Kubernetes ecosystem for
Kubernetes API server admission webhooks. Runtime Extensions share the same foundation and most of the same
considerations/concerns apply.

## Implementation

As mentioned above as a developer building systems on top of Cluster API, if you want to hook in the Cluster’s 
lifecycle via a Runtime Extension, you have to implement an HTTPS server handling a discovery request and a set 
of additional requests according to the OpenAPI specification for the Runtime Hook you are interested in.

The following shows a minimal example of a Runtime Extension server implementation:

```go
package main

import (
	"context"
	"flag"
	"net/http"
	"os"

	"github.com/spf13/pflag"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"

	runtimecatalog "sigs.k8s.io/cluster-api/exp/runtime/catalog"
	runtimehooksv1 "sigs.k8s.io/cluster-api/exp/runtime/hooks/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/runtime/server"
)

var (
	catalog = runtimecatalog.New()
	setupLog = ctrl.Log.WithName("setup")

	// Flags.
	profilerAddress string
	webhookPort     int
	webhookCertDir  string
	logOptions      = logs.NewOptions()
)

func init() {
	// Register the Runtime Hook types into the catalog.
	_ = runtimehooksv1.AddToCatalog(catalog)
}

// InitFlags initializes the flags.
func InitFlags(fs *pflag.FlagSet) {
	logs.AddFlags(fs, logs.SkipLoggingConfigurationFlags())
	logOptions.AddFlags(fs)

	fs.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")

	fs.IntVar(&webhookPort, "webhook-port", 9443,
		"Webhook Server port")

	fs.StringVar(&webhookCertDir, "webhook-cert-dir", "/tmp/k8s-webhook-server/serving-certs/",
		"Webhook cert dir, only used when webhook-port is specified.")
}

func main() {
	InitFlags(pflag.CommandLine)
	pflag.CommandLine.SetNormalizeFunc(cliflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	if err := logOptions.ValidateAndApply(nil); err != nil {
		setupLog.Error(err, "unable to start extension")
		os.Exit(1)
	}

	// klog.Background will automatically use the right logger.
	ctrl.SetLogger(klog.Background())

	if profilerAddress != "" {
		klog.Infof("Profiler listening for requests at %s", profilerAddress)
		go func() {
			klog.Info(http.ListenAndServe(profilerAddress, nil))
		}()
	}

	ctx := ctrl.SetupSignalHandler()

	webhookServer, err := server.NewServer(server.Options{
		Catalog: catalog,
		Port:    webhookPort,
		CertDir: webhookCertDir,
	})
	if err != nil {
		setupLog.Error(err, "error creating webhook server")
		os.Exit(1)
	}

	// Register extension handlers.
	if err := webhookServer.AddExtensionHandler(server.ExtensionHandler{
		Hook:           runtimehooksv1.BeforeClusterCreate,
		Name:           "before-cluster-create",
		HandlerFunc:    DoBeforeClusterCreate,
		TimeoutSeconds: pointer.Int32(5),
		FailurePolicy:  toPtr(runtimehooksv1.FailurePolicyFail),
	}); err != nil {
		setupLog.Error(err, "error adding handler")
		os.Exit(1)
	}
	if err := webhookServer.AddExtensionHandler(server.ExtensionHandler{
		Hook:           runtimehooksv1.BeforeClusterUpgrade,
		Name:           "before-cluster-upgrade",
		HandlerFunc:    DoBeforeClusterUpgrade,
		TimeoutSeconds: pointer.Int32(5),
		FailurePolicy:  toPtr(runtimehooksv1.FailurePolicyFail),
	}); err != nil {
		setupLog.Error(err, "error adding handler")
		os.Exit(1)
	}

	setupLog.Info("Starting Runtime Extension server")
	if err := webhookServer.Start(ctx); err != nil {
		setupLog.Error(err, "error running webhook server")
		os.Exit(1)
	}
}

func DoBeforeClusterCreate(ctx context.Context, request *runtimehooksv1.BeforeClusterCreateRequest, response *runtimehooksv1.BeforeClusterCreateResponse) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("BeforeClusterCreate is called")
	// Your implementation
}

func DoBeforeClusterUpgrade(ctx context.Context, request *runtimehooksv1.BeforeClusterUpgradeRequest, response *runtimehooksv1.BeforeClusterUpgradeResponse) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("BeforeClusterUpgrade is called")
	// Your implementation
}

func toPtr(f runtimehooksv1.FailurePolicy) *runtimehooksv1.FailurePolicy {
	return &f
}
```

For a full example see our [test extension](https://github.com/kubernetes-sigs/cluster-api/tree/main/test/extension). 

Please note that a Runtime Extension server can serve multiple Runtime Hooks (in the example above 
`BeforeClusterCreate` and `BeforeClusterUpgrade`) at the same time. Each of them are handled at a different path, like the 
Kubernetes API server does for different API resources. The exact format of those paths is handled by the server
automatically in accordance to the OpenAPI specification of the Runtime Hooks.

There is an additional `Discovery` endpoint which is automatically served by the `Server`. The `Discovery` endpoint
returns a list of extension handlers to inform Cluster API which Runtime Hooks are implemented by this 
Runtime Extension server.

Please note that Cluster API is only able to enforce the correct request and response types as defined by a Runtime Hook version.
Developers are fully responsible for all other elements of the design of a Runtime Extension implementation, including:

- To choose which programming language to use; please note that Golang is the language of choice, and we are not planning 
  to test or provide tooling and libraries for other languages. Nevertheless, given that we rely on Open API and plain 
  HTTPS calls, other languages should just work but support will be provided at best effort.
- To choose if a dedicated or a shared HTTPS Server is used for the Runtime Extension (it can be e.g. also used to serve a 
  metric endpoint).

When using Golang the Runtime Extension developer can benefit from the following packages (provided by the 
`sigs.k8s.io/cluster-api` module) as shown in the example above:

- `exp/runtime/hooks/api/v1alpha1` contains the Runtime Hook Golang API types, which are also used to generate the 
  OpenAPI specification.
- `exp/runtime/catalog` provides the `Catalog` object to register Runtime Hook definitions. The `Catalog` is then 
  used by the `server` package to handle requests. `Catalog` is similar to the `runtime.Scheme` of the 
  `k8s.io/apimachinery/pkg/runtime` package, but it is designed to store Runtime Hook registrations.
- `exp/runtime/server` provides a `Server` object which makes it easy to implement a Runtime Extension server.
  The `Server` will automatically handle tasks like Marshalling/Unmarshalling requests and responses. A Runtime 
  Extension developer only has to implement a strongly typed function that contains the actual logic.

## Guidelines

While writing a Runtime Extension the following important guidelines must be considered:

### Timeouts

Runtime Extension processing adds to reconcile durations of Cluster API controllers. They should respond to requests 
as quickly as possible, typically in milliseconds. Runtime Extension developers can decide how long the Cluster API Runtime 
should wait for a Runtime Extension to respond before treating the call as a failure (max is 30s) by returning the timeout 
during discovery. Of course a Runtime Extension can trigger long-running tasks in the background, but they shouldn't block 
synchronously. 

### Availability

Runtime Extension failure could result in errors in handling the workload clusters lifecycle, and so the implementation
should be robust, have proper error handling, avoid panics, etc.. . Failure policies can be set up to mitigate the 
negative impact of a Runtime Extension on the Cluster API Runtime, but this option can’t be used in all cases 
(see [Error Management](#error-management)).

### Blocking Hooks

A Runtime Hook can be defined as "blocking" -  e.g. the `BeforeClusterUpgrade` hook allows a Runtime Extension
to prevent the upgrade from starting. A Runtime Extension registered for the `BeforeClusterUpgrade` hook 
can block by returning a non-zero `retryAfterSeconds` value. Following consideration apply:

- The system might decide to retry the same Runtime Extension even before the `retryAfterSeconds` period expires,
  e.g. due to other changes in the Cluster, so `retryAfterSeconds` should be considered as an approximate maximum
  time before the next reconcile.
- If there is more than one Runtime Extension registered for the same Runtime Hook and more than one returns
  `retryAfterSeconds`, the shortest non-zero value will be used.
- If there is more than one Runtime Extension registered for the same Runtime Hook and at least one returns
  `retryAfterSeconds`, all Runtime Extensions will be called again.

Detailed description of what "blocking" means for each specific Runtime Hooks is documented case by case 
in the hook-specific implementation documentation (e.g. [Implementing Lifecycle Hook Runtime Extensions](./implement-lifecycle-hooks.md#Definitions)).

### Side Effects

It is recommended that Runtime Extensions should avoid side effects if possible, which means they should operate 
only on the content of the request sent to them, and not make out-of-band changes. If side effects are required, 
rules defined in the following sections apply.

### Idempotence

An idempotent Runtime Extension is able to succeed even in case it has already been completed before (the Runtime 
Extension checks current state and changes it only if necessary). This is necessary because a Runtime Extension 
may be called many times after it already succeeded because other Runtime Extensions for the same hook may not 
succeed in the same reconcile.

A practical example that explains why idempotence is relevant is the fact that extensions could be called more 
than once for the same lifecycle transition, e.g.

- Two Runtime Extensions are registered for the `BeforeClusterUpgrade` hook.
- Before a Cluster upgrade is started both extensions are called, but one of them temporarily blocks the operation 
  by asking to retry after 30 seconds.
- After 30 seconds the system retries the lifecycle transition, and both extensions are called again to re-evaluate
  if it is now possible to proceed with the Cluster upgrade.

### Avoid dependencies

Each Runtime Extension should accomplish its task without depending on other Runtime Extensions. Introducing 
dependencies across Runtime Extensions makes the system fragile, and it is probably a consequence of poor 
"Separation of Concerns" between extensions.

### Deterministic result

A deterministic Runtime Extension is implemented in such a way that given the same input it will always return
the same output.

Some Runtime Hooks, e.g. like external patches, might explicitly request for corresponding Runtime Extensions 
to support this property. But we encourage developers to follow this pattern more generally given that it fits 
well with practices like unit testing and generally makes the entire system more predictable and easier to troubleshoot.

### Error Management

In case a Runtime Extension returns an error, the error will be handled according to the corresponding failure policy
defined in the response of the Discovery call.

If the failure policy is `Ignore` the error is going to be recorded in the controller's logs, but the processing 
will continue. However we recognize that this failure policy cannot be used in most of the use cases because Runtime 
Extension implementers want to ensure that the task implemented by an extension is completed before continuing with 
the cluster's lifecycle.

If instead the failure policy is `Fail` the system will retry the operation until it passes. The following general 
considerations apply:

- It is the responsibility of Cluster API components to surface Runtime Extension errors using conditions.
- Operations will be retried with an exponential backoff or whenever the state of a Cluster changes (we are going to rely
  on controller runtime exponential backoff/watches).
- If there is more than one Runtime Extension registered for the same Runtime Hook and at least one of them fails,
  all the registered Runtime Extension will be retried. See [Idempotence](#idempotence)

Additional considerations about errors that apply only to a specific Runtime Hook will be documented in the hook-specific 
implementation documentation.
