package hyperbytedb

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// The operator renders a cluster's config and pods for the HyperbyteDB
// version it runs, so one operator manages clusters on several versions.
// A newer database rejects config keys it does not know, and an older one
// silently ignores keys it does not know (running on its defaults), so
// neither direction is safe without knowing the version.

// DatabaseVersionAnnotation pins the HyperbyteDB version the operator
// renders for, when neither spec.version nor the image tag is a version
// (a locally built image tagged "e2e", say).
const DatabaseVersionAnnotation = "hyperbytedb.hyperbyte.cloud/database-version"

// LatestDatabaseVersion is the newest HyperbyteDB release this operator
// knows. A cluster whose version cannot be determined is rendered for it.
const LatestDatabaseVersion = "0.9.3-beta"

// OldestDatabaseVersion is the oldest HyperbyteDB release this operator
// renders for. Older ones are rendered as this one.
const OldestDatabaseVersion = "0.8.0"

// Capability is something the operator only renders for a database that
// has it.
type Capability string

const (
	// [chdb] query_pool_size and write_pool_size. Before it there is only
	// pool_size, for both.
	CapabilitySplitChdbPools Capability = "split-chdb-pools"
	// An unset query pool is sized by the database from its CPUs, so the
	// operator leaves it unset.
	CapabilityAutoQueryPool Capability = "auto-query-pool"
	// The [sharding] section and sharded clusters.
	CapabilitySharding Capability = "sharding"
	// The control listener on server port + 1 (control_port).
	CapabilityControlListener Capability = "control-listener"
	// Evacuating several departing nodes at once.
	CapabilityEvacuateTogether Capability = "evacuate-together"
	// auth.cluster_token and auth.admin_username / admin_password: a
	// cluster with auth on can reach its own internal routes.
	CapabilityClusterAuth Capability = "cluster-auth"
	// The proxy serves /healthz, /readyz, /metrics and /admin on its own
	// admin listener (HYPERBYTEDB_PROXY_ADMIN_LISTEN) and answers 404 for
	// them on its client port. A proxy capability: see ProxySupports.
	CapabilityProxyAdminListener Capability = "proxy-admin-listener"
)

// CompatibilityEntry is one row of the compatibility matrix.
type CompatibilityEntry struct {
	Capability Capability
	// Since is the first HyperbyteDB version that has it.
	Since string
	// Without says what the operator does for an older database.
	Without string
}

// version090Beta introduced most of the capabilities below.
const version090Beta = "0.9.0-beta"

// sourceAssumed is a DatabaseVersion's Source when no version was found.
const sourceAssumed = "assumed"

// CompatibilityMatrix lists what each capability needs. docs/compatibility.md
// is generated from the same facts; keep them in step.
var CompatibilityMatrix = []CompatibilityEntry{
	{CapabilitySplitChdbPools, version090Beta, "writes pool_size (spec.chdb.poolSize, else queryPoolSize, else 1) for both pools"},
	{CapabilityAutoQueryPool, version090Beta, "an unset query pool is written as pool_size = 1, as operator v0.1.x did"},
	{CapabilitySharding, version090Beta, "omits the [sharding] section; spec.sharding has no effect"},
	{CapabilityControlListener, version090Beta, "ignores the control-listener annotation (no control port, liveness on the server port)"},
	{CapabilityEvacuateTogether, version090Beta, "ignores the evacuate-together annotation (departing nodes leave one at a time)"},
	{CapabilityClusterAuth, "0.9.1-beta", "renders auth.enabled only; no auth Secret, so a cluster with auth on cannot reach its internal routes"},
	{CapabilityProxyAdminListener, version090Beta, "probes the proxy and calls its admin API on its client port, where proxies before 0.9.0-beta serve them"},
}

// DatabaseVersion is the HyperbyteDB version a cluster is rendered for.
type DatabaseVersion struct {
	Version *semver.Version
	// Where it came from: "annotation", "spec.version", "image tag", or
	// "assumed" (the newest this operator knows).
	Source string
	// Raw is what Source said, before parsing.
	Raw string
}

func (d DatabaseVersion) String() string {
	return d.Version.Original()
}

func parseDatabaseVersion(raw string) (*semver.Version, bool) {
	v, err := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(raw), "v"))
	if err != nil {
		return nil, false
	}
	return v, true
}

func imageTag(image string) string {
	image = strings.TrimSpace(image)
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	slash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= slash {
		return ""
	}
	return image[colon+1:]
}

// ResolveDatabaseVersion is the version a cluster runs: the annotation,
// spec.version, or the image tag, the first that is a version; else the
// newest this operator knows.
func ResolveDatabaseVersion(cluster *v1alpha1.HyperbytedbCluster) DatabaseVersion {
	candidates := []struct{ source, raw string }{
		{"annotation", cluster.Annotations[DatabaseVersionAnnotation]},
		{"spec.version", cluster.Spec.Version},
		{"image tag", imageTag(ResolveHyperbytedbImage(cluster))},
	}
	for _, c := range candidates {
		if c.raw == "" {
			continue
		}
		if v, ok := parseDatabaseVersion(c.raw); ok {
			return DatabaseVersion{Version: v, Source: c.source, Raw: c.raw}
		}
	}
	v, _ := parseDatabaseVersion(LatestDatabaseVersion)
	return DatabaseVersion{Version: v, Source: sourceAssumed, Raw: LatestDatabaseVersion}
}

// ResolveProxyVersion is the version of the proxy the cluster runs: the tag
// of its proxy image when that is a version (spec.proxy.image may pin one),
// else the cluster's database version, which the proxy image follows.
func ResolveProxyVersion(cluster *v1alpha1.HyperbytedbCluster) *semver.Version {
	if v, ok := parseDatabaseVersion(imageTag(ResolveProxyImage(cluster))); ok {
		return v
	}
	return ResolveDatabaseVersion(cluster).Version
}

// ProxySupports reports whether the cluster's proxy version has the
// capability.
func ProxySupports(cluster *v1alpha1.HyperbytedbCluster, capability Capability) bool {
	return versionSupports(ResolveProxyVersion(cluster), capability)
}

// Supports reports whether the cluster's database version has the capability.
func Supports(cluster *v1alpha1.HyperbytedbCluster, capability Capability) bool {
	return versionSupports(ResolveDatabaseVersion(cluster).Version, capability)
}

func versionSupports(v *semver.Version, capability Capability) bool {
	for _, e := range CompatibilityMatrix {
		if e.Capability != capability {
			continue
		}
		since, ok := parseDatabaseVersion(e.Since)
		return ok && v.Compare(since) >= 0
	}
	return false
}

// CompatibilitySummary describes, for a status condition, the version a
// cluster is rendered for and what is left out for it.
func CompatibilitySummary(cluster *v1alpha1.HyperbytedbCluster) (reason, message string) {
	dv := ResolveDatabaseVersion(cluster)
	var missing []string
	for _, e := range CompatibilityMatrix {
		if !versionSupports(dv.Version, e.Capability) {
			missing = append(missing, string(e.Capability))
		}
	}
	reason = "Supported"
	switch {
	case dv.Source == sourceAssumed:
		reason = "Assumed"
	case dv.Version.Compare(semver.MustParse(OldestDatabaseVersion)) < 0:
		reason = "OlderThanSupported"
	case dv.Version.Compare(semver.MustParse(LatestDatabaseVersion)) > 0:
		reason = "NewerThanKnown"
	}
	message = fmt.Sprintf("HyperbyteDB %s (from %s)", dv, dv.Source)
	if dv.Source == sourceAssumed {
		message += fmt.Sprintf("; set %s or a version image tag to pin it", DatabaseVersionAnnotation)
	}
	if len(missing) > 0 {
		message += "; not rendered for this version: " + strings.Join(missing, ", ")
	}
	return reason, message
}
