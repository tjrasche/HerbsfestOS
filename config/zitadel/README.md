# ZITADEL production deployment

ZITADEL will serve `https://auth.r-und-t.app` in namespace `rundt`.
The independent `config/overlays/auth-prod` overlay contains the identity
provider; the festival application's existing basic auth remains in place.
Application OIDC integration and account/role mapping are separate work.

The official [ZITADEL Helm chart 10.3.0](https://github.com/zitadel/zitadel-charts/releases/tag/zitadel-10.3.0)
pins the API and Login UI to `v4.19.2`. It requires Kubernetes 1.30 or newer.
One API replica, one Login UI replica, and a dedicated PostgreSQL 17.11 CNPG
instance with 5Gi Longhorn storage match this cluster's small deployments.
This provides no high availability. Database backups are not configured yet;
configure and test backups before relying on this instance for production SSO.

CNPG creates the `zitadel` database owned by the `zitadel` application role
and generates `zitadel-postgres-app`. [CNPG bootstrap](https://cloudnative-pg.io/docs/1.26/bootstrap/)
assigns database ownership to this role. The Helm init job runs
`zitadel init zitadel`, the chart-supported alias for schema initialization.
[ZITADEL database requirements](https://zitadel.com/docs/self-hosting/manage/database)
confirm that schema initialization needs only database owner privileges.
Superuser access stays disabled. The API, init, and setup workloads read the
application password directly from CNPG's Secret and verify database TLS
against the public `ca.crt` projected from `zitadel-postgres-ca`. They do not
mount the CA's private key.

Traefik terminates external HTTPS and uses h2c to the ZITADEL API; this is
required for its gRPC/HTTP2 endpoints. `/ui/v2/login` routes to the separate
Login UI on service port 3000; other paths route to the API on 8080.
The chart generates the Login UI's RSA credentials and registers its
`IAM_LOGIN_CLIENT` system user, as described in the [Login V2 documentation](https://zitadel.com/docs/self-hosting/manage/adopt-login-v2).
No bootstrap PAT or extra IAM owner machine user is needed.

The initial human administrator is `t@rasche-thalhofer.cloud`, with a verified
email and a required password change on first login. Self-registration is
disabled. SMTP and external identity providers are not configured; add these
through the ZITADEL console when needed. `DefaultInstance`/`FirstInstance`
settings bootstrap a new database; editing them later does not update existing
users or instance policies.

## Secrets

`sealedsecret-bootstrap.yaml` contains only ciphertext, scoped strictly to
`rundt/zitadel-bootstrap` using this production cluster's Sealed Secrets
certificate. It provides `masterkey`, `admin-password`, and
`ZITADEL_SESSION_COOKIE_SECRET`. The random initial password can be retrieved
privately from the host after Sealed Secrets has created the Secret:

```sh
bash scripts/zitadel.sh password YOUR_PRODUCTION_CONTEXT
```

The command intentionally prints the initial password. Store it privately,
sign into `/ui/console`, and change it. This Secret is not a record of the
account's current password after that change.

The masterkey must stay identical for the lifetime of the database. Back it up
in a secure password manager alongside database recovery material; never
regenerate it to resolve a failed deployment. A database backup without the
masterkey cannot recover encrypted ZITADEL data. Keep the Sealed Secrets
controller's recovery keys backed up through the existing cluster procedure.

To encrypt the existing bootstrap values with the current controller
certificate, preserving the masterkey and cookie key:

```sh
bash scripts/zitadel.sh reseal YOUR_PRODUCTION_CONTEXT
```

This fetches a public certificate, reads the existing Secret through a pipe,
validates the ciphertext, and updates the tracked SealedSecret. It writes no
plaintext secrets. Commit the result to deploy the new encryption. To move to
another cluster, first restore the original bootstrap Secret through your
secure recovery procedure, then reseal against that cluster. Changing the
bootstrap admin password in YAML does not rotate an existing user password;
use the console. Cookie-key rotation should retain the old key as the second
comma-separated entry during the transition, as documented by the chart.

## Register and verify later

The existing application Flux Kustomization remains unchanged. The separate
`config/flux/zitadel.yaml` registers `rundt-zitadel` against the existing
`herbsfest` GitRepository, with dependencies on this cluster's namespace and
operators. It is deliberately outside the application's reconciled overlay,
so committing these files alone does not install ZITADEL.

For lasting GitOps ownership, include this Flux integration manifest in the
infrastructure repository's OVH app inventory, following its existing
`rundt-herbsfest` registration. The host `register` action below is an initial
bootstrap fallback; it does not update the infrastructure inventory.

Before registration, publish the manifests to the GitRepository's watched
`main` branch and point DNS for `auth.r-und-t.app` to the same
Traefik origin endpoint as the festival application. For direct ingress access,
disable BunnyCDN Acceleration for this DNS record; use the cluster
load-balancer address rather than a resolved CDN edge address. If routing
through BunnyCDN, configure its custom hostname and edge certificate separately.
The existing `bunnycdn-issuer` must be able to issue `zitadel-tls` for
`auth.r-und-t.app`. Then run on the host:

```sh
bash scripts/zitadel.sh register YOUR_PRODUCTION_CONTEXT
bash scripts/zitadel.sh check YOUR_PRODUCTION_CONTEXT
```

`register` validates the bootstrap ciphertext against the selected cluster's
controller before applying the Flux integration manifest.
`check` reconciles Flux, waits for CNPG, Helm, both Deployments and the
certificate, and requests OIDC discovery. Open
`https://auth.r-und-t.app/ui/console` to verify the interactive login
and forced password change. Before chart upgrades, back up the database and
review [ZITADEL operations](https://zitadel.com/docs/self-hosting/deploy/kubernetes/operations).

The database disables Flux pruning to retain identity data if the integration
is removed. Explicit database deletion is a separate operation. Preserve the
bootstrap Secret and its masterkey before removing any auth resources.

Render the auth overlay without contacting a cluster:

```sh
kustomize build config/overlays/auth-prod
```

This renders Flux resources and their values; Helm renders the workload
manifests when the release reconciles. The auth hostname appears in
`helmrelease.yaml` (issuer and Login V2 base URI), `ingress.yaml`, and
`scripts/zitadel.sh` (discovery check).
