# HerbsfestOS

Barebones server-rendered Go application for managing a nonprofit festival.
The first domain is a feedback note: a specific feedback point to track.
Create and list notes at `/`; htmx updates the page without a full reload.
The same form works with JavaScript disabled.

## Stack and structure

- Go 1.26+, standard `net/http` routing and cross-origin protection.
- templ for HTML, self-hosted htmx 2.0.11, plain CSS, embedded assets.
- GORM with its PostgreSQL driver.
- ko for container builds; Kustomize for Kubernetes configuration.

```text
cmd/web/                 dependency wiring, server lifecycle, migration command
internal/feedbacknote/   entity, service, repository, handler, templates
internal/database/       database connection and pool
internal/ui/             shared layout and embedded frontend assets
config/app/              application Deployment, Service, configuration
config/postgres/         plain development Postgres StatefulSet and storage
config/cnpg/             production CloudNativePG Cluster
config/overlays/dev/     development namespace and database credentials
config/overlays/prod/    production ingress, auth, CNPG and deployment patches
config/overlays/auth-prod/ ZITADEL identity provider and its database
config/flux/            Flux registration for the identity provider
config/zitadel/         pinned ZITADEL Helm release, ingress and sealed bootstrap
config/kind/             local cluster configuration
scripts/prod.sh          host-side basic-auth credential sealing
```

Add a package under `internal/` for each new domain entity. Keep its service,
repository, HTTP handler, and templates together. The service depends on a small
repository interface; the concrete implementation uses GORM. There are no shared
service/repository layers, dependency injection frameworks, or router libraries.
Generated `*_templ.go` files are checked in so ordinary `go build` works.
The pinned templ CLI is available through `go tool templ`.

## Develop in kind

Install Go, Docker 28+ (API 1.48+), kubectl, and the following tools. Make sure Go's binary
directory is in your `PATH`. Docker must be running.

```sh
go install github.com/google/ko@v0.19.1
go install sigs.k8s.io/kustomize/kustomize/v5@v5.8.2
go install sigs.k8s.io/kind@v0.31.0

make dev
make dev-forward
```

Open <http://localhost:8080>. `make dev` creates the `herbsfest` kind cluster,
generates templates, builds the application with ko, loads the application and
Postgres images directly into kind, applies the dev overlay, and waits for the workloads. No registry or
Dockerfile is required. The build targets the kind node's architecture, including
Apple Silicon. Dev Postgres uses a 1Gi persistent volume and deliberately
public development credentials. kind's default storage provisioner handles it.
Postgres is exported as an archive containing only the node's platform, which
also works with Docker's containerd image store.

After editing Go, templ, CSS, or configuration, run `make dev-deploy` again.
The image contains all frontend assets. You may need to restart port forwarding
when the pod is replaced. Override the cluster name with
`make dev KIND_CLUSTER_NAME=my-cluster`; use that name for subsequent targets.
Every kubectl target specifies `kind-<name>` explicitly.

```sh
make test              # generate, test, vet
make build             # binary at bin/web
make render            # dev YAML, with unresolved ko:// image references
make render OVERLAY=prod # complete app YAML with pinned image and sealed auth
make kind-down         # deletes the local cluster and its database data
```

## Design system

The feedback page uses a shared German application shell with responsive
navigation. Visit `/design-system` for the live component gallery: colors,
typography, buttons, badges, cards, form fields, notices, and empty states.

Shared templ components live in `internal/ui/components.templ`; icons live in
`internal/ui/icons.templ`. Wrap a domain page in `ui.Layout(title, activePage)`
and keep its content/htmx fragments in the domain package. The feedback page
demonstrates this: htmx swaps `#notes`, while the navigation and layout remain
outside the fragment. Buttons accept templ attributes for native form behavior.
Forms remain usable without JavaScript.

The footer thanks Rasche & Thalhofer UG (haftungsbeschränkt) for providing free
hosting. Its locally embedded logo comes from
[rasche-thalhofer.cloud](https://rasche-thalhofer.cloud/assets/logo.svg.svg),
and the credit uses that website's blue palette.

Color, spacing, radius, and typography tokens are defined in
`internal/ui/static/style.css`. Blue `#1565c0`, hover blue `#2d84e8`, orange
`#ff5722`, and Source Sans 3 come from the
[festival homepage](https://ringinger-herbstfest.de/). Deep blue `#0f4787` comes
from its anniversary banner; the neutral colors are new app colors.

The original mascot SVG and Latin font subset are served locally from
`internal/ui/static/brand/`, including the font's SIL Open Font License.
`app.js` only adds focus restoration and a save announcement after htmx swaps.
There is no CSS framework or frontend build pipeline. Run `make dev-deploy`
after changes to rebuild the image and embedded assets.

## Run Go directly

To use the development database from kind:

```sh
kubectl --context kind-herbsfest -n herbsfest-dev port-forward service/postgres 5432:5432
```

In another terminal:

```sh
export DATABASE_URL='postgres://herbsfest:herbsfest@localhost:5432/herbsfest?sslmode=disable'
make migrate
make run
```

`HTTP_ADDR` defaults to `:8080`. Environment variables are read directly;
`.env.example` is a reference, not an automatically loaded file.
`/healthz` checks the HTTP process and `/readyz` checks the database connection.

## Production configuration

ZITADEL's initial SSO infrastructure is prepared separately at
`config/overlays/auth-prod`, using `auth.r-und-t.app`. See
[the ZITADEL setup guide](config/zitadel/README.md) for DNS, Flux registration,
bootstrap credentials, and host commands. The festival app keeps its current
authentication while the identity provider is prepared.

The prod overlay follows the existing apps in
[k8s-infra](https://github.com/rasche-thalhofer/k8s-infra/):

- Existing namespace `rundt` and image pull Secret `oci-registry-cred`.
- Image repository `artifacts.r-und-t.app/herbsfest`, built by ko.
- Traefik serves `https://herbstfest.r-und-t.app`; HTTP redirects
  to HTTPS before authentication. All HTTPS paths require basic auth.
- cert-manager issues `herbsfest-tls` using `bunnycdn-issuer`. The `r-und-t.app` DNS
  zone must be accessible to that issuer's Bunny DNS credentials.
- A single CNPG instance with 5Gi Longhorn storage, matching the small apps in
  the infrastructure repo. CNPG creates `herbsfest-postgres-app`; the app and
  migration container read its `uri` key. This is not an HA database setup.
- Traefik reads bcrypt credentials from `herbsfest-basic-auth`, removes the
  Authorization header before forwarding, and sends `Cache-Control: private, no-store`.

The cluster needs Traefik, cert-manager, CloudNativePG, Sealed Secrets, and
Longhorn installed by the infrastructure repository. The overlay reuses its
namespace and registry Secret; it does not create or take ownership of them.

### GitOps deployment

This repository owns the production app manifests, including the sealed
basic-auth Secret, in `config/overlays/prod`. The infrastructure repository
registers the public app repository as Flux source `herbsfest`; the existing OVH
Kustomization `rundt-herbsfest` reconciles this overlay. Both poll every minute.
The shared namespace, registry pull Secret, and cluster operators stay in infra.

Every push to `main` runs `.github/workflows/ci.yml`:

1. Generate templates, run Go tests and vet, and check generated files are committed.
2. Build with ko for `linux/amd64` and push to `artifacts.r-und-t.app/herbsfest`,
   tagged with the source commit SHA.
3. Commit the published image digest into the production Kustomize overlay.
   The same override updates both the web and migration containers.
4. Flux reads that commit and rolls out the image to production.

Pull requests only run checks. CI never needs Kubernetes credentials. Publishing
uses these repository Actions secrets (already configured):

- `OCI_REGISTRY_URL`
- `OCI_REGISTRY_USER`
- `OCI_REGISTRY_PASSWORD`

The digest commit uses this repo's built-in `GITHUB_TOKEN` with `contents: write`.
No GitHub App or cross-repository workflow dispatch is needed. GitHub suppresses
new workflow runs for token-generated pushes; the commit also includes `[skip ci]`.
Repository rules must allow the Actions bot to push the digest update to `main`.

Builds queue without cancellation (up to GitHub's 100 pending-run limit). Each
revision publishes an image, but only a build whose source SHA still matches
`main` can push a deployment update. A newer commit winning that race prevents
an older build from replacing production. CI success confirms publishing and
the Git update; Flux readiness is checked separately.

To retry a release, run the `CI` workflow manually on `main`. To roll back, commit
the desired previously published digest in `config/overlays/prod/kustomization.yaml`
with `[skip ci]` in the commit message so CI doesn't rebuild over the rollback.
Flux applies the committed digest. A later normal source push resumes releases.

The database Cluster disables Flux pruning to retain festival data if the app
is removed. Database deletion is a separate explicit operation. CNPG backups
are not configured yet.

### Host-side production tools

Use `scripts/check-prod.sh` on your host to reconcile and check the deployment:

```sh
bash scripts/check-prod.sh YOUR_PRODUCTION_CONTEXT
```

This needs `flux` and `kubectl` and checks CNPG, the Deployment, and the TLS
certificate. The source must have been registered by the infra reconciliation.

To rotate basic-auth credentials, install `kubeseal` and Apache `htpasswd`.
On macOS, `brew install kubeseal httpd` provides them; add
`$(brew --prefix httpd)/bin` to `PATH` if needed. Run on your host:

```sh
bash scripts/prod.sh cert YOUR_PRODUCTION_CONTEXT
bash scripts/prod.sh seal YOUR_PRODUCTION_CONTEXT
```

The seal command prompts for the password for `mvr`, fetches the public
certificate from `sealed-secrets-controller` in namespace `sealed-secrets`,
and validates the result against that cluster. It updates
`config/overlays/prod/sealedsecret-basic-auth.yaml`; commit that encrypted file
to deploy the new credentials. Plain passwords and unencrypted Secret manifests
never reach disk. The ciphertext is scoped to `rundt/herbsfest-basic-auth`.

`make render OVERLAY=prod` renders the complete app configuration, including the
committed digest and SealedSecret. Development still uses ko to build local images.

After deployment, HTTP should redirect to HTTPS, and an unauthenticated HTTPS
request should return `401` with a basic-auth challenge:

```sh
curl -I http://herbstfest.r-und-t.app/
curl -I https://herbstfest.r-und-t.app/
curl --user mvr https://herbstfest.r-und-t.app/ # prompts for password
```

The migration command uses GORM `AutoMigrate` for the initial scaffold and runs
in an init container. It exits on connection or migration errors; Kubernetes
retries while the database starts. Use explicit versioned migrations before
making destructive schema changes or scaling concurrent schema-changing rolls.

## References

- [templ code generation](https://templ.guide/core-concepts/template-generation/)
- [htmx documentation](https://htmx.org/docs/) (the vendored license is next to the JS file)
- [GORM PostgreSQL setup](https://gorm.io/docs/connecting_to_the_database.html)
- [ko configuration and kind integration](https://ko.build/configuration/)
- [CNPG application connection Secrets](https://cloudnative-pg.io/documentation/1.26/applications/)
