package hyperbytedb

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func StatefulSetName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name
}

// BuildStatefulSet constructs the full StatefulSet for a HyperbytedbCluster.
// configHash is a short digest from hyperbytedb.ConfigHash (replica-independent);
// it is placed as a pod annotation so static config edits trigger a rolling restart.
func BuildStatefulSet(cluster *v1alpha1.HyperbytedbCluster, configHash string) *appsv1.StatefulSet {
	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}
	port := serverPort(cluster)
	clusterEnabled := replicas > 1
	headlessSvc := HeadlessServiceName(cluster)

	image := cluster.Spec.Image
	if image == "" {
		image = "hyperbytedb:latest"
	}

	pullPolicy := cluster.Spec.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	// --------------- init script ---------------
	// Reads peer addresses from the operator-managed peers ConfigMap mounted
	// at /peers instead of computing them from a hard-coded replica count.
	initScript := `#!/bin/sh
set -e
ORDINAL=$(echo "$HOSTNAME" | rev | cut -d'-' -f1 | rev)
NODE_ID=$((ORDINAL + 1))
echo "export HYPERBYTEDB__CLUSTER__NODE_ID=$NODE_ID" > /shared/env.sh

# Own address is the entry matching our ordinal in the peers ConfigMap
SELF_ADDR=$(cat /peers/${ORDINAL} 2>/dev/null || echo "")
echo "export HYPERBYTEDB__CLUSTER__CLUSTER_ADDR=${SELF_ADDR}" >> /shared/env.sh

# Build peer list from all entries except our own
PEERS=""
for f in /peers/*; do
  KEY=$(basename "$f")
  # Skip non-numeric keys (e.g. "replicas")
  case "$KEY" in
    *[!0-9]*) continue ;;
  esac
  if [ "$KEY" != "$ORDINAL" ]; then
    ADDR=$(cat "$f")
    if [ -n "$PEERS" ]; then
      PEERS="${PEERS},${ADDR}"
    else
      PEERS="${ADDR}"
    fi
  fi
done
echo "export HYPERBYTEDB__CLUSTER__PEERS=${PEERS}" >> /shared/env.sh
`

	entrypoint := `#!/bin/sh
. /shared/env.sh
exec hyperbytedb --config /etc/hyperbytedb/config.toml serve
`

	// --------------- volumes ---------------
	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: ConfigMapName(cluster),
					},
				},
			},
		},
		{
			Name: "peers",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: PeersConfigMapName(cluster),
					},
				},
			},
		},
		{
			Name: "shared",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "entrypoint",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	// TLS volume
	if cluster.Spec.Server.TLS != nil && cluster.Spec.Server.TLS.Enabled {
		secretName := cluster.Spec.Server.TLS.SecretName
		if secretName == "" {
			secretName = cluster.Name + "-tls"
		}
		volumes = append(volumes, corev1.Volume{
			Name: "tls",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})
	}

	// User-provided additional volumes
	volumes = append(volumes, cluster.Spec.AdditionalVolumes...)

	// --------------- init containers ---------------
	initContainers := []corev1.Container{
		{
			Name:    "init-node-id",
			Image:   "busybox:1.36",
			Command: []string{"sh", "-c", initScript},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "shared", MountPath: "/shared"},
				{Name: "peers", MountPath: "/peers", ReadOnly: true},
			},
		},
		{
			Name:    "init-entrypoint",
			Image:   "busybox:1.36",
			Command: []string{"sh", "-c", "printf '%s' '" + entrypoint + "' > /entrypoint/start.sh && chmod +x /entrypoint/start.sh"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "entrypoint", MountPath: "/entrypoint"},
			},
		},
	}

	// --------------- env vars ---------------
	env := []corev1.EnvVar{
		{Name: "HYPERBYTEDB__STORAGE__DATA_DIR", Value: "/var/lib/hyperbytedb/data"},
		{Name: "HYPERBYTEDB__STORAGE__WAL_DIR", Value: "/var/lib/hyperbytedb/wal"},
		{Name: "HYPERBYTEDB__STORAGE__META_DIR", Value: "/var/lib/hyperbytedb/meta"},
		{Name: "HYPERBYTEDB__CHDB__SESSION_DATA_PATH", Value: "/var/lib/hyperbytedb/chdb"},
	}

	// Cluster mode and paths come from mounted config.toml (hot-updated on scale);
	// avoid replica-dependent env vars so the pod template stays stable across scale events.

	if cluster.Spec.Storage.S3 != nil && cluster.Spec.Storage.S3.CredentialsSecretName != "" {
		env = append(env,
			corev1.EnvVar{
				Name: "HYPERBYTEDB__STORAGE__S3__ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: cluster.Spec.Storage.S3.CredentialsSecretName},
						Key:                  "access_key_id",
					},
				},
			},
			corev1.EnvVar{
				Name: "HYPERBYTEDB__STORAGE__S3__SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: cluster.Spec.Storage.S3.CredentialsSecretName},
						Key:                  "secret_access_key",
					},
				},
			},
		)
	}

	if cluster.Spec.Server.TLS != nil && cluster.Spec.Server.TLS.Enabled {
		env = append(env,
			corev1.EnvVar{Name: "HYPERBYTEDB__SERVER__TLS__ENABLED", Value: "true"},
			corev1.EnvVar{Name: "HYPERBYTEDB__SERVER__TLS__CERT_FILE", Value: "/etc/hyperbytedb/tls/tls.crt"},
			corev1.EnvVar{Name: "HYPERBYTEDB__SERVER__TLS__KEY_FILE", Value: "/etc/hyperbytedb/tls/tls.key"},
		)
	}

	// --------------- volume mounts ---------------
	volumeMounts := []corev1.VolumeMount{
		{Name: "data", MountPath: "/var/lib/hyperbytedb"},
		{Name: "config", MountPath: "/etc/hyperbytedb", ReadOnly: true},
		{Name: "shared", MountPath: "/shared", ReadOnly: true},
		{Name: "entrypoint", MountPath: "/entrypoint", ReadOnly: true},
	}

	if cluster.Spec.Server.TLS != nil && cluster.Spec.Server.TLS.Enabled {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "tls",
			MountPath: "/etc/hyperbytedb/tls",
			ReadOnly:  true,
		})
	}

	volumeMounts = append(volumeMounts, cluster.Spec.AdditionalVolumeMounts...)

	// --------------- probes ---------------
	probeScheme := corev1.URISchemeHTTP
	if cluster.Spec.Server.TLS != nil && cluster.Spec.Server.TLS.Enabled {
		probeScheme = corev1.URISchemeHTTPS
	}

	livenessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/ping",
				Port:   intstr.FromString("http"),
				Scheme: probeScheme,
			},
		},
		InitialDelaySeconds: 15,
		PeriodSeconds:       10,
		TimeoutSeconds:      3,
	}

	readinessProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/health",
				Port:   intstr.FromString("http"),
				Scheme: probeScheme,
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
		TimeoutSeconds:      3,
	}

	// --------------- main container ---------------
	mainContainer := corev1.Container{
		Name:            "hyperbytedb",
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command:         []string{"sh", "/entrypoint/start.sh"},
		Env:             env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts:   volumeMounts,
		LivenessProbe:  livenessProbe,
		ReadinessProbe: readinessProbe,
		Resources:      cluster.Spec.Resources,
	}

	if clusterEnabled {
		mainContainer.Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"sh", "-c",
						fmt.Sprintf("curl -sf -X POST http://localhost:%d/internal/drain || true; sleep 45", port)},
				},
			},
		}
	}

	// --------------- PVC ---------------
	storageSize := resource.MustParse("10Gi")
	if cluster.Spec.Storage.VolumeClaimTemplate != nil && !cluster.Spec.Storage.VolumeClaimTemplate.Size.IsZero() {
		storageSize = cluster.Spec.Storage.VolumeClaimTemplate.Size
	}

	pvcSpec := corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: storageSize,
			},
		},
	}

	if cluster.Spec.Storage.VolumeClaimTemplate != nil && cluster.Spec.Storage.VolumeClaimTemplate.StorageClassName != nil {
		pvcSpec.StorageClassName = cluster.Spec.Storage.VolumeClaimTemplate.StorageClassName
	}

	// --------------- pod template labels & annotations ---------------
	podLabels := make(map[string]string)
	for k, v := range CommonLabels(cluster) {
		podLabels[k] = v
	}
	for k, v := range cluster.Spec.PodLabels {
		podLabels[k] = v
	}

	podAnnotations := make(map[string]string)
	for k, v := range cluster.Spec.PodAnnotations {
		podAnnotations[k] = v
	}
	if configHash != "" {
		podAnnotations["hyperbytedb.hyperbytedb.io/config-hash"] = configHash
	}

	// --------------- pod spec ---------------
	podSpec := corev1.PodSpec{
		TerminationGracePeriodSeconds: ptr.To(int64(60)),
		ImagePullSecrets:              cluster.Spec.ImagePullSecrets,
		InitContainers:                initContainers,
		Containers:                    []corev1.Container{mainContainer},
		Volumes:                       volumes,
		Tolerations:                   cluster.Spec.Tolerations,
	}

	if cluster.Spec.Affinity != nil {
		podSpec.Affinity = cluster.Spec.Affinity
	} else if clusterEnabled {
		// Default: spread pods across nodes for HA
		podSpec.Affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: CommonLabels(cluster),
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		}
	}

	if len(cluster.Spec.TopologySpreadConstraints) > 0 {
		podSpec.TopologySpreadConstraints = cluster.Spec.TopologySpreadConstraints
	}

	// --------------- StatefulSet ---------------
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StatefulSetName(cluster),
			Namespace: cluster.Namespace,
			Labels:    CommonLabels(cluster),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    ptr.To(replicas),
			ServiceName: headlessSvc,
			Selector: &metav1.LabelSelector{
				MatchLabels: CommonLabels(cluster),
			},
			PodManagementPolicy: podManagementPolicy(clusterEnabled),
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "data",
					},
					Spec: pvcSpec,
				},
			},
		},
	}

	return sts
}

func podManagementPolicy(clusterEnabled bool) appsv1.PodManagementPolicyType {
	if clusterEnabled {
		return appsv1.OrderedReadyPodManagement
	}
	return appsv1.ParallelPodManagement
}
