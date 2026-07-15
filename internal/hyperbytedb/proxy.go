package hyperbytedb

import (
	"fmt"
	"maps"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// ProxyDeploymentName / ProxyServiceName follow the StatefulSet/headless-svc
// pattern: `<cluster>-proxy` for the Deployment, `<cluster>-proxy` for the
// stable client-facing Service. The original `<cluster>` headless Service
// stays in place so the proxy can still discover backend pods via DNS.
func ProxyDeploymentName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name + "-proxy"
}

func ProxyServiceName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name + "-proxy"
}

// ProxyServiceAddr returns the in-cluster DNS name for the proxy Service.
func ProxyServiceAddr(cluster *v1alpha1.HyperbytedbCluster) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", ProxyServiceName(cluster), cluster.Namespace)
}

// ProxyEnabled reports whether the operator should reconcile proxy resources.
// The proxy is enabled by default; set spec.proxy.enabled=false to opt out.
func ProxyEnabled(cluster *v1alpha1.HyperbytedbCluster) bool {
	if cluster.Spec.Proxy == nil {
		return true
	}
	return cluster.Spec.Proxy.Enabled
}

// proxyLabels intentionally uses `name=hyperbytedb-proxy` instead of
// `name=hyperbytedb`. The headless Service selector is just CommonLabels
// (`name=hyperbytedb`, `instance=<name>`, `managed-by=hyperbytedb-operator`)
// and a StatefulSet/Service selector is immutable, so we cannot retro-fit a
// `component=database` filter onto the existing headless selector. By
// flipping `name` on the proxy side we guarantee the headless DNS only
// resolves to database pods — without that, the proxy resolves itself,
// forwards requests to itself, and infinitely recurses until the pod OOMs.
func proxyLabels(cluster *v1alpha1.HyperbytedbCluster) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "hyperbytedb-proxy",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/managed-by": "hyperbytedb-operator",
		"app.kubernetes.io/component":  "proxy",
	}
}

func proxyPort(cluster *v1alpha1.HyperbytedbCluster) int32 {
	if cluster.Spec.Proxy != nil && cluster.Spec.Proxy.Port > 0 {
		return cluster.Spec.Proxy.Port
	}
	return ServerPort(cluster)
}

// BuildProxyService is the stable client entry point for the cluster when
// proxy mode is enabled. Defaults to ClusterIP; supports NodePort/LoadBalancer
// for clusters that need external exposure (kind, on-prem, etc.).
func BuildProxyService(cluster *v1alpha1.HyperbytedbCluster) *corev1.Service {
	spec := cluster.Spec.Proxy
	port := proxyPort(cluster)

	svcType := corev1.ServiceTypeClusterIP
	if spec != nil && spec.ServiceType != "" {
		svcType = spec.ServiceType
	}

	svcPort := corev1.ServicePort{
		Name:       "http",
		Port:       port,
		TargetPort: intstr.FromString("http"),
		Protocol:   corev1.ProtocolTCP,
	}
	if svcType == corev1.ServiceTypeNodePort && spec != nil && spec.NodePort > 0 {
		svcPort.NodePort = spec.NodePort
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ProxyServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    proxyLabels(cluster),
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: proxyLabels(cluster),
			Ports:    []corev1.ServicePort{svcPort},
		},
	}
}

// BuildProxyDeployment renders the stateless proxy Deployment that fans out
// in front of the StatefulSet. The proxy discovers pods via the existing
// headless Service so we don't need any extra wiring as the cluster scales.
func BuildProxyDeployment(cluster *v1alpha1.HyperbytedbCluster) *appsv1.Deployment {
	spec := cluster.Spec.Proxy
	if spec == nil {
		// Defensive: callers should gate on ProxyEnabled, but never panic.
		spec = &v1alpha1.ProxySpec{}
	}

	replicas := int32(2)
	if spec.Replicas != nil {
		replicas = *spec.Replicas
	}

	image := ResolveProxyImage(cluster)
	pullPolicy := spec.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	port := proxyPort(cluster)
	backendPort := ServerPort(cluster)
	backendService := fmt.Sprintf("%s.%s.svc.cluster.local",
		HeadlessServiceName(cluster), cluster.Namespace)

	holdSecs := int32(30)
	if spec.HoldTimeoutSecs > 0 {
		holdSecs = spec.HoldTimeoutSecs
	}
	maxRetries := int32(2)
	if spec.MaxRetries >= 0 && spec.MaxRetries != 0 {
		maxRetries = spec.MaxRetries
	}
	graceSecs := int32(30)
	if spec.ShutdownGraceSecs > 0 {
		graceSecs = spec.ShutdownGraceSecs
	}
	requestTimeout := cluster.Spec.Server.RequestTimeoutSecs
	if requestTimeout <= 0 {
		requestTimeout = 60
	}
	if spec.RequestTimeoutSecs > 0 {
		requestTimeout = spec.RequestTimeoutSecs
	}
	healthPath := spec.HealthPath
	if healthPath == "" {
		healthPath = "/health"
	}

	env := []corev1.EnvVar{
		{Name: "HYPERBYTEDB_PROXY_LISTEN", Value: fmt.Sprintf("0.0.0.0:%d", port)},
		{Name: "HYPERBYTEDB_PROXY_BACKEND_SERVICE", Value: backendService},
		{Name: "HYPERBYTEDB_PROXY_BACKEND_PORT", Value: strconv.Itoa(int(backendPort))},
		{Name: "HYPERBYTEDB_PROXY_HEALTH_PATH", Value: healthPath},
		{Name: "HYPERBYTEDB_PROXY_HOLD_TIMEOUT_SECS", Value: strconv.Itoa(int(holdSecs))},
		{Name: "HYPERBYTEDB_PROXY_MAX_RETRIES", Value: strconv.Itoa(int(maxRetries))},
		{Name: "HYPERBYTEDB_PROXY_SHUTDOWN_GRACE_SECS", Value: strconv.Itoa(int(graceSecs))},
		{Name: "HYPERBYTEDB_PROXY_REQUEST_TIMEOUT_SECS", Value: strconv.Itoa(int(requestTimeout))},
		// Downward-API: defense-in-depth so the proxy never adds its own pod
		// IP to the backend pool even if a future label refactor accidentally
		// makes the headless Service select proxy pods again.
		{
			Name: "HYPERBYTEDB_PROXY_SELF_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
	}
	if cluster.Spec.Logging.Format == "json" {
		env = append(env, corev1.EnvVar{Name: "LOG_FORMAT", Value: "json"})
	}

	podLabels := make(map[string]string)
	maps.Copy(podLabels, proxyLabels(cluster))
	maps.Copy(podLabels, spec.PodLabels)

	podAnnotations := map[string]string{}
	maps.Copy(podAnnotations, spec.PodAnnotations)

	container := corev1.Container{
		Name:            "proxy",
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromString("http"),
				},
			},
			InitialDelaySeconds: 2,
			PeriodSeconds:       10,
			TimeoutSeconds:      2,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/readyz",
					Port: intstr.FromString("http"),
				},
			},
			InitialDelaySeconds: 1,
			PeriodSeconds:       2,
			TimeoutSeconds:      2,
		},
		Resources: spec.Resources,
	}

	// Spread proxy replicas across nodes for HA. Cheap default — users can
	// override via spec.Proxy.PodLabels + topology constraints applied
	// out-of-band if they need something tighter.
	podSpec := corev1.PodSpec{
		// Long enough to absorb in-flight queries during a proxy restart.
		// The proxy itself enforces shutdown_grace internally; this just
		// gives the kubelet headroom before SIGKILL.
		TerminationGracePeriodSeconds: ptr.To(int64(graceSecs + 10)),
		ImagePullSecrets:              spec.ImagePullSecrets,
		Containers:                    []corev1.Container{container},
		Affinity: &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: proxyLabels(cluster),
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		},
	}

	maxUnavailable := intstr.FromInt(0)
	maxSurge := intstr.FromInt(1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ProxyDeploymentName(cluster),
			Namespace: cluster.Namespace,
			Labels:    proxyLabels(cluster),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: proxyLabels(cluster),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: &maxUnavailable,
					MaxSurge:       &maxSurge,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: podSpec,
			},
		},
	}
}
