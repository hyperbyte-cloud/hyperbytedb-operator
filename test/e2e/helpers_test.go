//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"strings"
)

func singleNodeCR(name string) *strings.Reader {
	return strings.NewReader(fmt.Sprintf(`apiVersion: hyperbytedb.hyperbytedb.io/v1alpha1
kind: HyperbytedbCluster
metadata:
  name: %s
  namespace: %s
spec:
  replicas: 1
  image: hyperbytedb:latest
  imagePullPolicy: Never
  server:
    port: 8086
  storage:
    volumeClaimTemplate:
      size: 1Gi
  logging:
    level: info
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
  monitoring:
    enabled: false
    serviceMonitor: false
`, name, testNamespace))
}

func clusterCR(name string, replicas int) *strings.Reader {
	return strings.NewReader(fmt.Sprintf(`apiVersion: hyperbytedb.hyperbytedb.io/v1alpha1
kind: HyperbytedbCluster
metadata:
  name: %s
  namespace: %s
spec:
  replicas: %d
  image: hyperbytedb:latest
  imagePullPolicy: Never
  server:
    port: 8086
  storage:
    volumeClaimTemplate:
      size: 1Gi
  logging:
    level: info
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 512Mi
  monitoring:
    enabled: false
    serviceMonitor: false
`, name, testNamespace, replicas))
}

func backupCR(name, clusterName string) *strings.Reader {
	return strings.NewReader(fmt.Sprintf(`apiVersion: hyperbytedb.hyperbytedb.io/v1alpha1
kind: HyperbytedbBackup
metadata:
  name: %s
  namespace: %s
spec:
  clusterName: %s
  destination:
    s3:
      bucket: test-backups
      prefix: "e2e/"
  retentionDays: 1
`, name, testNamespace, clusterName))
}

func restoreCR(name, clusterName, backupName string) *strings.Reader {
	return strings.NewReader(fmt.Sprintf(`apiVersion: hyperbytedb.hyperbytedb.io/v1alpha1
kind: HyperbytedbRestore
metadata:
  name: %s
  namespace: %s
spec:
  clusterName: %s
  backupName: %s
`, name, testNamespace, clusterName, backupName))
}
