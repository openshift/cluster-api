# capi-ipam-conversion-webhook

This standalone webhook converts IPAddress and IPAddressClaim resources between
`v1alpha1`, `v1beta1`, and `v1beta2` using the existing Cluster API conversion code.
It lets Machine API Provider vSphere use CAPI IPAM without running the full CAPI
manager. It does not run CAPI controllers, admission webhooks, or leader election.

## Deployment

The [release bundle](../manifests/0000_30_cluster-api_03_capi-ipam-conversion-webhook.yaml)
deploys one replica with dedicated RBAC, Service, and NetworkPolicy in the shared
`openshift-cluster-api` namespace supplied by the Cluster CAPI Operator (CCAPIO).
It requires the `ClusterAPIOperator` feature gate and supports self-managed HA
and single-node installations. Hosted installations are outside this deployment's
scope.

The binary ships in the existing `cluster-capi-controllers` image. Its Deployment
runs `/capi-ipam-conversion-webhook`; `/manager` remains the image's default
entrypoint. The Service exposes HTTPS port 443 at `/convert` and forwards to pod
port 9443. HTTPS health endpoints are `/healthz` and `/readyz`.

OpenShift service-ca supplies the serving certificate and injects the CRD CA
bundle. Certificate rotation is reloaded without restarting the process. The
webhook follows the cluster TLS profile and adherence policy; changes shut down
the process so Kubernetes restarts it with the updated settings.

## CRD lifecycle

With both `ClusterAPIOperator` enabled and `ClusterAPIMachineManagement` disabled,
the [new standalone IPAM CRDs](../ipam/kustomization.yaml) use this Service. All
three API versions are served; `v1beta1` remains the sole storage version for
rollback compatibility. With both feature gates disabled, the
[legacy standalone CRDs](../manifests/0000_30_cluster-api_04_crd.core-cluster-api-legacy.yaml)
remain installed without conversion webhook configuration.

The full-provider CRDs retain their existing conversion endpoint and
`capi-operator.openshift.io/adopt-existing: "always"` annotation, allowing CCAPIO
to take over existing CRDs later. Storage promotion and retirement of this
standalone webhook are future work. Retirement requires a working full CAPI
conversion backend and switching both CRDs to it before deleting the standalone
resources. Existing CRDs, stored objects, and the shared namespace must be preserved.

## Development and validation

Run the race-enabled unit and envtest integration tests and regenerate the
standalone CRDs from the repository root. The test target uses the repository's
envtest setup to download API-server and etcd binaries when needed:

```sh
make -C openshift test-capi-ipam-conversion-webhook
make -C openshift ipam-manifests
```

Unit tests do not establish bootstrap readiness. Release acceptance also requires
fresh Default HA and single-node installation and upgrade checks covering namespace
availability, scheduling, certificates, networking, all supported conversions with
the full manager absent, certificate rotation, and TLS policy changes. Check
capability exclusion and the separate full-provider endpoint as well.
