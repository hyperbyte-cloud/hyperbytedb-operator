#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
HYPERBYTEDB_DIR="$(cd "${PROJECT_DIR}/.." && pwd)"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-hyperbytedb-operator-dev}"
OPERATOR_IMG="${OPERATOR_IMG:-hyperbytedb-operator:dev}"
HYPERBYTEDB_IMG="${HYPERBYTEDB_IMG:-hyperbytedb:latest}"

echo "=== Hyperbytedb Operator - Kind Setup ==="

# 1. Create kind cluster
if kind get clusters 2>/dev/null | grep -q "^${KIND_CLUSTER_NAME}$"; then
    echo "Kind cluster '${KIND_CLUSTER_NAME}' already exists, skipping creation."
else
    echo "Creating kind cluster '${KIND_CLUSTER_NAME}'..."
    kind create cluster --name "${KIND_CLUSTER_NAME}" --config "${SCRIPT_DIR}/kind-config.yaml"
fi

# 2. Build hyperbytedb image
echo "Building hyperbytedb Docker image..."
docker build -t "${HYPERBYTEDB_IMG}" "${HYPERBYTEDB_DIR}"

# 3. Build operator image
echo "Building operator Docker image..."
docker build -t "${OPERATOR_IMG}" "${PROJECT_DIR}"

# 4. Load images into kind
echo "Loading images into kind cluster..."
kind load docker-image "${HYPERBYTEDB_IMG}" --name "${KIND_CLUSTER_NAME}"
kind load docker-image "${OPERATOR_IMG}" --name "${KIND_CLUSTER_NAME}"

# 5. Install CRDs
echo "Installing CRDs..."
cd "${PROJECT_DIR}"
make install

# 6. Deploy operator
echo "Deploying operator..."
make deploy IMG="${OPERATOR_IMG}"

# 7. Wait for operator to be ready
echo "Waiting for operator to be ready..."
kubectl -n hyperbytedb-operator-system wait --for=condition=available deployment/hyperbytedb-operator-controller-manager --timeout=120s

echo ""
echo "=== Setup complete ==="
echo "  Kind cluster: ${KIND_CLUSTER_NAME}"
echo "  Hyperbytedb image: ${HYPERBYTEDB_IMG}"
echo "  Operator image: ${OPERATOR_IMG}"
echo ""
echo "Apply a sample CR:"
echo "  kubectl apply -f config/samples/hyperbytedb_v1alpha1_hyperbytedbcluster_single.yaml"
echo "  kubectl apply -f config/samples/hyperbytedb_v1alpha1_hyperbytedbcluster_cluster.yaml"
