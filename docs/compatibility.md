# HyperbyteDB compatibility

One operator manages clusters on several HyperbyteDB versions. For each
cluster it works out which version runs and renders the config and pods for
that version: a HyperbyteDB 0.9 rejects config keys it does not know, and a
0.8 ignores them and runs on its defaults, so neither is safe to get keys
meant for the other.

## Which version a cluster runs

The first of these that is a version (`0.8.5-beta`, `v0.9.0-beta`, `0.9.0`):

1. the annotation `hyperbytedb.hyperbyte.cloud/database-version`,
2. `spec.version`,
3. the tag of the database image.

If none is (a local image tagged `e2e`, say), the cluster is rendered for the
newest version this operator knows. The `DatabaseVersion` condition in the
cluster's status says which version was used, where it came from, and what
was left out for it:

```sh
kubectl get hyperbytedbcluster my-cluster \
  -o jsonpath='{.status.conditions[?(@.type=="DatabaseVersion")]}'
```

Reasons: `Supported`, `Assumed` (no version found), `NewerThanKnown`
(rendered for the newest this operator knows), `OlderThanSupported`
(condition status `False`; rendered as the oldest supported).

## Matrix

| Operator | Renders for HyperbyteDB | Newest known |
|---|---|---|
| v0.3.2 | 0.8.0 and later | 0.9.1-beta |
| v0.3.1 | 0.8.0 and later | 0.9.1-beta |
| v0.3.0 | 0.8.0 and later | 0.9.1-beta |
| v0.2.0 | 0.8.0 and later | 0.9.0-beta |
| v0.1.x | not version-aware: renders the newest keys for every cluster (0.8.x ignores the ones it does not know) | — |

What v0.3.2 renders per version:

| Capability | HyperbyteDB | For older versions the operator |
|---|---|---|
| Split chDB pools (`query_pool_size`, `write_pool_size`) | 0.9.0-beta | writes `pool_size` for both: `spec.chdb.poolSize`, else `queryPoolSize`, else 1 |
| Query pool sized by the database from its CPUs | 0.9.0-beta | writes `pool_size = 1` when no pool field is set, as v0.1.x did |
| Sharding (`[sharding]`, `spec.sharding`) | 0.9.0-beta | omits the section; `spec.sharding` has no effect |
| Control listener (`control-listener` annotation) | 0.9.0-beta | ignores the annotation; liveness stays on the server port |
| Evacuate together (`evacuate-together` annotation) | 0.9.0-beta | ignores the annotation; departing nodes leave one at a time |
| Cluster auth (`spec.auth.enabled`: auth Secret, cluster token, bootstrap admin) | 0.9.1-beta | renders `auth.enabled` only; no Secret, so a cluster with auth on cannot reach its own internal routes |
| Proxy admin listener (probes, `/metrics`, `/admin` on port 8087; the proxy's version is its image tag, else the cluster's) | 0.9.0-beta (proxy) | probes the proxy and calls its admin API on the proxy's client port, where older proxies serve them |
| Parallel pod management (`parallel-pods` annotation) | any | — (operator only) |

The source of truth is `CompatibilityMatrix` in
`internal/hyperbytedb/compat.go`; keep this page in step with it.

## Upgrading

- v0.3.1 → v0.3.2: a pool stuck in a rolling restart recovers by itself.
  The proxy Deployment is applied before the rolling restart, so a broken
  proxy is fixed even while a restart waits on it; proxies are excluded
  from and included in a restart when their process runs, Ready or not
  (a one-node pool's proxies are not Ready while its only backend is
  excluded), and proxies that are not running are skipped; a restart that
  has excluded nothing while no update is pending is cleared. A `Degraded`
  condition is set back to `False` once a reconcile completes. No pod
  template changes.
- v0.3.0 → v0.3.1: a proxy on 0.9.0-beta or later is probed on its admin
  listener (port 8087, or 8088 when the proxy's client port is 8087).
  v0.3.0 probed `/healthz` and `/readyz` on the client port, where those
  proxies answer 404, so the proxy restarted forever, never became ready,
  and every request through it failed with 502. The proxy Deployment of
  such a cluster rolls once; a 0.8.x proxy's does not change. The admin
  port is not on the proxy's Service.
- v0.2.0 → v0.3.0: cluster auth needs HyperbyteDB 0.9.1-beta (0.9.0-beta
  has no cluster token and refuses the key). A cluster with
  `spec.auth.enabled` that resolves to 0.9.0-beta gets no auth Secret env,
  so move it to 0.9.1-beta (version and image) before or with the operator.
- Upgrading the operator does not restart a cluster unless its rendered
  config or pods change. v0.1.x → v0.2.0 renders the same config for a
  0.8.5-beta cluster and for a 0.9 development cluster that sets its pool
  sizes; a 0.9 cluster with no pool field changes (it stops writing
  `pool_size = 1` and lets the database size its query pool), which rolls
  its pods once.
- Upgrading HyperbyteDB: change `spec.version` (or the image). The operator
  renders for the new version in the same reconcile, so the new pods start
  with a config they accept.
- Do not run `SET` statements while a HyperbyteDB rolling upgrade is in
  progress: nodes on the older build cannot decode them.
