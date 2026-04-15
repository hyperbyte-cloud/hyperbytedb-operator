# HyperbyteDB Operator

Kubernetes operator for managing HyperbyteDB clusters, backups, and restores.

## Prerequisites

- Kubernetes v1.26+
- kubectl
- Helm v3 (optional, for Helm-based installation)

## Quick Start

### Install with Helm

```sh
helm install hyperbytedb-operator dist/chart \
  --namespace hyperbytedb-operator-system \
  --create-namespace
```

### Install with kubectl

```sh
kubectl apply -f https://raw.githubusercontent.com/hyperbyte-cloud/hyperbytedb-operator/main/dist/install.yaml
```

### Deploy a Cluster

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
