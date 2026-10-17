package hyperbytedb

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// With spec.auth.enabled, a cluster's internal routes need an admin login.
// Nodes and the operator have none, so they send a shared cluster token
// instead. The operator keeps the token and a bootstrap admin login in a
// Secret, passes them to the pods as env, and sends the token on its own
// calls.

// Keys of the auth Secret.
const (
	AuthSecretClusterTokenKey  = "cluster-token"
	AuthSecretAdminUsernameKey = "admin-username"
	AuthSecretAdminPasswordKey = "admin-password"
)

// ClusterTokenHeader carries the cluster token on internal routes.
const ClusterTokenHeader = "x-hyperbytedb-cluster-token"

// AuthSecretName is spec.auth.credentialsSecretName, else <cluster>-auth.
func AuthSecretName(cluster *v1alpha1.HyperbytedbCluster) string {
	if cluster.Spec.Auth.CredentialsSecretName != "" {
		return cluster.Spec.Auth.CredentialsSecretName
	}
	return cluster.Name + "-auth"
}

// ClusterAuthEnabled is whether the operator wires the auth Secret into
// the cluster: auth is on and the database knows the cluster token.
func ClusterAuthEnabled(cluster *v1alpha1.HyperbytedbCluster) bool {
	return cluster.Spec.Auth.Enabled && Supports(cluster, CapabilityClusterAuth)
}

func randomSecret(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// FillAuthSecret adds any missing key to secret's data: a random token, the
// admin user "admin" and a random password. Existing values are kept, so a
// user-supplied Secret needs only the keys it wants to choose. Returns
// whether it changed anything.
func FillAuthSecret(secret *corev1.Secret) (bool, error) {
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	changed := false
	for _, k := range []struct {
		key   string
		value func() (string, error)
	}{
		{AuthSecretClusterTokenKey, func() (string, error) { return randomSecret(32) }},
		{AuthSecretAdminUsernameKey, func() (string, error) { return "admin", nil }},
		{AuthSecretAdminPasswordKey, func() (string, error) { return randomSecret(24) }},
	} {
		if len(secret.Data[k.key]) > 0 {
			continue
		}
		v, err := k.value()
		if err != nil {
			return false, err
		}
		secret.Data[k.key] = []byte(v)
		changed = true
	}
	return changed, nil
}

// BuildAuthSecret is a new auth Secret with every key generated.
func BuildAuthSecret(cluster *v1alpha1.HyperbytedbCluster) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AuthSecretName(cluster),
			Namespace: cluster.Namespace,
			Labels:    CommonLabels(cluster),
		},
		Type: corev1.SecretTypeOpaque,
	}
	if _, err := FillAuthSecret(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// authEnv passes the auth Secret to the database.
func authEnv(cluster *v1alpha1.HyperbytedbCluster) []corev1.EnvVar {
	from := func(name, key string) corev1.EnvVar {
		return corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: AuthSecretName(cluster)},
				Key:                  key,
			}},
		}
	}
	return []corev1.EnvVar{
		from("HYPERBYTEDB__AUTH__CLUSTER_TOKEN", AuthSecretClusterTokenKey),
		from("HYPERBYTEDB__AUTH__ADMIN_USERNAME", AuthSecretAdminUsernameKey),
		from("HYPERBYTEDB__AUTH__ADMIN_PASSWORD", AuthSecretAdminPasswordKey),
	}
}

type clusterTokenKey struct{}

// WithClusterToken makes the Client send token on requests made with ctx.
// One Client serves every cluster; the token is per cluster.
func WithClusterToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, clusterTokenKey{}, token)
}

// tokenTransport adds the request context's cluster token, if any.
type tokenTransport struct {
	base http.RoundTripper
}

func (t tokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, _ := req.Context().Value(clusterTokenKey{}).(string); token != "" {
		req = req.Clone(req.Context())
		req.Header.Set(ClusterTokenHeader, token)
	}
	return t.base.RoundTrip(req)
}
