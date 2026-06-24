package hyperbytedb

import (
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

const (
	// drainAckWaitSecs mirrors hyperbytedb DrainService::wait_for_replication_acks
	// max_wait. preStop must outlive that window so the pod is not SIGKILL'd mid-drain.
	drainAckWaitSecs = 90
	// podTerminationGraceSecs is the kubelet grace budget (includes preStop). Keep
	// comfortably above drainAckWaitSecs + headroom for flush + SIGTERM cleanup.
	podTerminationGraceSecs = 120
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

	image := ResolveHyperbytedbImage(cluster)

	pullPolicy := cluster.Spec.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	// --------------- init script ---------------
	// Derives this pod's NODE_ID and CLUSTER_ADDR from its StatefulSet
	// ordinal. Peer discovery is intentionally not done here: the operator
	// drives Raft membership via the /cluster/membership/add-node API as the
	// StatefulSet scales, so each pod starts up with no peer knowledge and
	// is added by the leader once it becomes reachable.
	initScript := fmt.Sprintf(`#!/bin/sh
set -e
ORDINAL=$(echo "$HOSTNAME" | rev | cut -d'-' -f1 | rev)
NODE_ID=$((ORDINAL + 1))
SELF_ADDR="${HOSTNAME}.%s.%s.svc.cluster.local:%d"
{
  echo "export HYPERBYTEDB__CLUSTER__NODE_ID=$NODE_ID"
  echo "export HYPERBYTEDB__CLUSTER__CLUSTER_ADDR=$SELF_ADDR"
  echo "export HYPERBYTEDB__CLUSTER__PEERS="
} > /shared/env.sh
`, headlessSvc, cluster.Namespace, port)

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
	chdbPath := defaultChdbSessionPath
	if cluster.Spec.ChDB.SessionDataPath != "" {
		chdbPath = cluster.Spec.ChDB.SessionDataPath
	}
	env := []corev1.EnvVar{
		{Name: "HYPERBYTEDB__STORAGE__WAL_DIR", Value: defaultWalDir},
		{Name: "HYPERBYTEDB__STORAGE__META_DIR", Value: defaultMetaDir},
		{Name: "HYPERBYTEDB__CHDB__SESSION_DATA_PATH", Value: chdbPath},
	}
	// Cluster mode and paths come from mounted config.toml (hot-updated on scale);
	// avoid replica-dependent env vars so the pod template stays stable across scale events.

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

	// Startup can be slow on a node with large series cardinality or a cold
	// chDB that must be rebuilt from the WAL: the HTTP listener only binds
	// after the dedup-cache warm and startup WAL replay complete. Without a
	// startup probe, the liveness probe (15s delay + 3×10s) fires at ~35s and
	// SIGKILLs the pod mid-warm, which restarts and re-does the same heavy
	// work — a self-amplifying CPU/memory loop. The startup probe holds
	// liveness off until /ping first answers, giving the listener up to
	// FailureThreshold×PeriodSeconds (600s) to come up before any restart.
	startupProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path:   "/ping",
				Port:   intstr.FromString("http"),
				Scheme: probeScheme,
			},
		},
		InitialDelaySeconds: 5,
		PeriodSeconds:       5,
		TimeoutSeconds:      3,
		FailureThreshold:    120,
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
		StartupProbe:   startupProbe,
		LivenessProbe:  livenessProbe,
		ReadinessProbe: readinessProbe,
		Resources:      cluster.Spec.Resources,
	}

	if clusterEnabled {
		mainContainer.Lifecycle = &corev1.Lifecycle{
			PreStop: &corev1.LifecycleHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"sh", "-c", clusterPreStopScript(port)},
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
	maps.Copy(podLabels, CommonLabels(cluster))
	maps.Copy(podLabels, cluster.Spec.PodLabels)

	podAnnotations := make(map[string]string)
	maps.Copy(podAnnotations, cluster.Spec.PodAnnotations)
	if configHash != "" {
		podAnnotations["hyperbytedb.hyperbytedb.io/config-hash"] = configHash
	}

	// --------------- pod spec ---------------
	podSpec := corev1.PodSpec{
		TerminationGracePeriodSeconds: ptr.To(int64(podTerminationGraceSecs)),
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

// clusterPreStopScript initiates graceful drain and blocks until the node
// reports state=leaving (flush + replication acks + peer leave complete) or
// until drainAckWaitSecs elapses. A fixed sleep is insufficient because
// DrainService may wait up to 60s for peer replication acks alone.
func clusterPreStopScript(port int32) string {
	return fmt.Sprintf(`curl -sf -X POST http://localhost:%[1]d/internal/drain || true
for i in $(seq 1 %[2]d); do
  if curl -sf http://localhost:%[1]d/cluster/metrics 2>/dev/null | grep -q '"state":"leaving"'; then
    exit 0
  fi
  sleep 1
done
exit 0`, port, drainAckWaitSecs)
}
