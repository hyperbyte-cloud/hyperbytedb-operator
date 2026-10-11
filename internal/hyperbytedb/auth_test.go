package hyperbytedb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func authCluster(version string, enabled bool) *v1alpha1.HyperbytedbCluster {
	c := versionedCluster(version, "", nil)
	c.Name = "uat"
	c.Namespace = "ns"
	c.Spec.Auth.Enabled = enabled
	return c
}

func hyperbytedbContainer(t *testing.T, cluster *v1alpha1.HyperbytedbCluster) corev1.Container {
	t.Helper()
	for _, c := range BuildStatefulSet(cluster, "hash").Spec.Template.Spec.Containers {
		if c.Name == "hyperbytedb" {
			return c
		}
	}
	t.Fatal("no hyperbytedb container")
	return corev1.Container{}
}

func TestFillAuthSecretKeepsWhatIsThereAndAddsTheRest(t *testing.T) {
	s := &corev1.Secret{Data: map[string][]byte{AuthSecretAdminPasswordKey: []byte("chosen")}}
	changed, err := FillAuthSecret(s)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if string(s.Data[AuthSecretAdminPasswordKey]) != "chosen" {
		t.Error("a chosen password was replaced")
	}
	if string(s.Data[AuthSecretAdminUsernameKey]) != "admin" {
		t.Errorf("admin username = %q", s.Data[AuthSecretAdminUsernameKey])
	}
	if len(s.Data[AuthSecretClusterTokenKey]) < 40 {
		t.Errorf("token too short: %q", s.Data[AuthSecretClusterTokenKey])
	}
	again, _ := FillAuthSecret(s)
	if again {
		t.Error("a full Secret was changed")
	}
	other, _ := BuildAuthSecret(authCluster("0.9.0-beta", true))
	if string(other.Data[AuthSecretClusterTokenKey]) == string(s.Data[AuthSecretClusterTokenKey]) {
		t.Error("two generated tokens are equal")
	}
}

func TestAuthSecretNameDefaultsToTheCluster(t *testing.T) {
	c := authCluster("0.9.0-beta", true)
	if got := AuthSecretName(c); got != "uat-auth" {
		t.Errorf("got %q", got)
	}
	c.Spec.Auth.CredentialsSecretName = "mine"
	if got := AuthSecretName(c); got != "mine" {
		t.Errorf("got %q", got)
	}
}

// With auth on, the pods get the Secret as env and the drain script sends
// the token. Off, or on a database without cluster auth, the pod template is
// what it was, so existing clusters are not restarted.
func TestClusterAuthReachesThePodOnlyWhenOnAndSupported(t *testing.T) {
	on := hyperbytedbContainer(t, authCluster("0.9.0-beta", true))
	refs := map[string]string{}
	for _, e := range on.Env {
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			refs[e.Name] = e.ValueFrom.SecretKeyRef.Name + "/" + e.ValueFrom.SecretKeyRef.Key
		}
	}
	want := map[string]string{
		"HYPERBYTEDB__AUTH__CLUSTER_TOKEN":  "uat-auth/cluster-token",
		"HYPERBYTEDB__AUTH__ADMIN_USERNAME": "uat-auth/admin-username",
		"HYPERBYTEDB__AUTH__ADMIN_PASSWORD": "uat-auth/admin-password",
	}
	if !reflect.DeepEqual(refs, want) {
		t.Errorf("secret env = %v, want %v", refs, want)
	}
	preStop := strings.Join(on.Lifecycle.PreStop.Exec.Command, " ")
	if strings.Count(preStop, ClusterTokenHeader+": $HYPERBYTEDB__AUTH__CLUSTER_TOKEN") != 2 {
		t.Errorf("preStop curls must send the token:\n%s", preStop)
	}

	off := hyperbytedbContainer(t, authCluster("0.9.0-beta", false))
	old := hyperbytedbContainer(t, authCluster("0.8.5-beta", true))
	for name, c := range map[string]corev1.Container{"auth off": off, "0.8.5-beta": old} {
		for _, e := range c.Env {
			if strings.HasPrefix(e.Name, "HYPERBYTEDB__AUTH__") {
				t.Errorf("%s: unexpected env %s", name, e.Name)
			}
		}
		if p := strings.Join(c.Lifecycle.PreStop.Exec.Command, " "); strings.Contains(p, ClusterTokenHeader) {
			t.Errorf("%s: preStop changed:\n%s", name, p)
		}
	}
	if !reflect.DeepEqual(off.Lifecycle, old.Lifecycle) {
		t.Error("preStop differs between auth off and an old database")
	}
}

func TestTheClientSendsTheTokenOnlyWithOne(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get(ClusterTokenHeader))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewClient()
	for _, ctx := range []context.Context{
		context.Background(),
		WithClusterToken(context.Background(), "tok"),
	} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if !reflect.DeepEqual(got, []string{"", "tok"}) {
		t.Errorf("headers seen = %q", got)
	}
}
