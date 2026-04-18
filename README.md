# HyperbyteDB Operator

Kubernetes operator for managing HyperbyteDB clusters, backups, and restores.

## Prerequisites

- Kubernetes v1.26+
- kubectl
- Helm v3.8+ (required for OCI registry support)

## Installation

The operator is published as both a container image and a Helm chart on
GitHub Container Registry (GHCR / GitHub Packages):

| Artifact          | Reference                                                              |
| ----------------- | ---------------------------------------------------------------------- |
| Operator image    | `ghcr.io/hyperbyte-cloud/hyperbytedb-operator`                         |
| Helm chart (OCI)  | `oci://ghcr.io/hyperbyte-cloud/charts/hyperbytedb-operator`            |

Both artifacts are released together — installing chart version `X.Y.Z` will
deploy the matching `vX.Y.Z` operator image by default.

### Install the latest release

```sh
helm install hyperbytedb-operator \
  oci://ghcr.io/hyperbyte-cloud/charts/hyperbytedb-operator \
  --namespace hyperbytedb-operator-system \
  --create-namespace
```

### Install a specific version

```sh
helm install hyperbytedb-operator \
  oci://ghcr.io/hyperbyte-cloud/charts/hyperbytedb-operator \
  --version 0.1.0 \
  --namespace hyperbytedb-operator-system \
  --create-namespace
```

You can browse all published versions on the
[hyperbytedb-operator packages page](https://github.com/orgs/hyperbyte-cloud/packages?repo_name=hyperbytedb-operator).

### Snapshot / pre-release builds

Every push to `main` publishes a snapshot chart and image tagged with the
short commit SHA, e.g. `0.0.0-sha.abc1234`. To install one:

```sh
helm install hyperbytedb-operator \
  oci://ghcr.io/hyperbyte-cloud/charts/hyperbytedb-operator \
  --version 0.0.0-sha.abc1234 \
  --namespace hyperbytedb-operator-system \
  --create-namespace
```

### Common configuration

```sh
helm install hyperbytedb-operator \
  oci://ghcr.io/hyperbyte-cloud/charts/hyperbytedb-operator \
  --namespace hyperbytedb-operator-system \
  --create-namespace \
  --set manager.replicas=2 \
  --set rbacHelpers.enable=true \
  --set prometheus.enable=true
```

See [`dist/chart/values.yaml`](dist/chart/values.yaml) for the full list of
configurable values.

### Upgrade

```sh
helm upgrade hyperbytedb-operator \
  oci://ghcr.io/hyperbyte-cloud/charts/hyperbytedb-operator \
  --version 0.2.0 \
  --namespace hyperbytedb-operator-system \
  --reuse-values
```

### Uninstall

```sh
helm uninstall hyperbytedb-operator --namespace hyperbytedb-operator-system
```

CRDs are retained on uninstall by default (`crd.keep=true`). To remove them
explicitly:

```sh
kubectl delete crd \
  hyperbytedbclusters.hyperbytedb.hyperbytedb.io \
  hyperbytedbbackups.hyperbytedb.hyperbytedb.io \
  hyperbytedbrestores.hyperbytedb.hyperbytedb.io
```

### Install from source (development)

To install directly from a checkout of this repository, e.g. for local
testing of un-released changes:

```sh
helm install hyperbytedb-operator dist/chart \
  --namespace hyperbytedb-operator-system \
  --create-namespace \
  --set manager.image.repository=ghcr.io/hyperbyte-cloud/hyperbytedb-operator \
  --set manager.image.tag=latest
```

## Deploy a Cluster

```yaml
apiVersion: hyperbytedb.hyperbytedb.io/v1alpha1
kind: HyperbytedbCluster
metadata:
  name: my-cluster
spec:
  replicas: 3
  image: hyperbytedb:latest
  storage:
    volumeClaimTemplate:
      size: 10Gi
  monitoring:
    enabled: true
    serviceMonitor: true
```

```sh
kubectl apply -f cluster.yaml
```

## Custom Resource Definitions

The operator manages three CRDs:

- **HyperbytedbCluster** -- Declares a HyperbyteDB cluster with configurable replicas, storage, networking, monitoring, autoscaling, and failover.
- **HyperbytedbBackup** -- Defines one-shot or scheduled backups to S3-compatible storage with configurable retention.
- **HyperbytedbRestore** -- Restores a cluster from an existing backup, coordinating scale-down, data copy, and scale-up automatically.

See `config/samples/` for example manifests.

## Development

```sh
# Build the operator binary
make build

# Run unit and integration tests
make test

# Build the container image
make docker-build IMG=<registry>/hyperbytedb-operator:<tag>

# Push the container image
make docker-push IMG=<registry>/hyperbytedb-operator:<tag>

# Install CRDs into the current cluster
make install

# Deploy the operator to the current cluster
make deploy IMG=<registry>/hyperbytedb-operator:<tag>

# Remove CRDs and the operator
make undeploy
```

Run `make help` for a full list of targets.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
