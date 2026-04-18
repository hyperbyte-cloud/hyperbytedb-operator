#!/usr/bin/env bash
set -euo pipefail

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-hyperbytedb-operator-dev}"

echo "Deleting kind cluster '${KIND_CLUSTER_NAME}'..."
kind delete cluster --name "${KIND_CLUSTER_NAME}"
echo "Done."
