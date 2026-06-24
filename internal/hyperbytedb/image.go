package hyperbytedb

import (
	"fmt"
	"strings"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

const (
	defaultHyperbytedbRepo = "hyperbytedb"
	defaultProxyRepo       = "hyperbytedb-proxy"
	defaultImageTag        = "latest"
)

// NormalizeVersionTag maps a semver-ish version string to an OCI image tag.
// "0.8.3" and "v0.8.3" both become "v0.8.3" to match published container tags.
func NormalizeVersionTag(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return defaultImageTag
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// ResolveHyperbytedbImage returns the container image for database pods.
//
// Resolution order:
//   - spec.image with a tag (contains ':') → used verbatim (local/kind overrides)
//   - spec.image without a tag → repository, tag from spec.version (or latest)
//   - spec.version only → hyperbytedb:{tag}
//   - neither → hyperbytedb:latest
func ResolveHyperbytedbImage(cluster *v1alpha1.HyperbytedbCluster) string {
	return resolveComponentImage(cluster.Spec.Image, cluster.Spec.Version, defaultHyperbytedbRepo)
}

// ResolveProxyImage returns the container image for hyperbytedb-proxy pods.
// Uses the same tag rules as ResolveHyperbytedbImage so proxy and database
// versions stay aligned when spec.proxy.image is not pinned.
func ResolveProxyImage(cluster *v1alpha1.HyperbytedbCluster) string {
	repoOverride := ""
	if cluster.Spec.Proxy != nil {
		repoOverride = cluster.Spec.Proxy.Image
	}
	return resolveComponentImage(repoOverride, cluster.Spec.Version, defaultProxyRepo)
}

func resolveComponentImage(imageField, version, defaultRepo string) string {
	tag := defaultImageTag
	if version != "" {
		tag = NormalizeVersionTag(version)
	}

	img := strings.TrimSpace(imageField)
	if img == "" {
		return fmt.Sprintf("%s:%s", defaultRepo, tag)
	}
	if strings.Contains(img, ":") {
		return img
	}
	return fmt.Sprintf("%s:%s", img, tag)
}
