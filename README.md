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
config/kind/             local cluster configuration
scripts/prod.sh          host-side sealing and production deployment
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
make render OVERLAY=prod # app YAML; sealed auth is generated separately below
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

The prod overlay follows the existing apps in
[k8s-infra](https://github.com/rasche-thalhofer/k8s-infra/):

- Existing namespace `rundt` and image pull Secret `oci-registry-cred`.
- Image repository `artifacts.r-und-t.app/herbsfest`, built by ko.
- Traefik serves `https://verwaltung.ringinger-herbstfest.de`; HTTP redirects
  to HTTPS before authentication. All HTTPS paths require basic auth.
- cert-manager issues `herbsfest-tls` using `bunnycdn-issuer`. The festival DNS
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

The infrastructure repository manages this app through the OVH Flux
Kustomization `rundt-herbsfest`, pointing at `flux/apps/rundt/prod/herbsfest`.
Its base is `flux/apps/rundt/base/herbsfest`, including the encrypted auth
Secret. The registry credentials remain in the existing `PROD_RUNDT` GitHub
environment in that repository.

The `build-herbsfest-image` workflow checks out this public app repository,
tests it, builds with ko, and commits the published image digest for both the
web container and migration container. The first successful build also
unsuspends the Flux entry, so Flux never attempts to deploy the bootstrap image.
To release another app revision, run that workflow in `k8s-infra` with the
desired branch, tag, or commit as `source_ref`. It records the exact app commit
in `flux/apps/rundt/base/herbsfest/source.json`. Updating that file in Git also
triggers the build. Flux then reconciles the new digest automatically.

### Host-side deployment tools

The commands below are available for sealing credentials and for a manual
deployment before adopting Flux. Once Flux owns the app, use its workflow to
release changes instead of applying the manual bundle.

Run the following **on your host**, where your production Kubernetes context
and registry credentials are available. Install `kubeseal` and Apache `htpasswd`
in addition to the development tools. On macOS, `brew install kubeseal httpd`
provides them; add `$(brew --prefix httpd)/bin` to `PATH` if needed.

```sh
kubectl config get-contexts

# If you only want to provide the public certificate to the sandbox:
bash scripts/prod.sh cert YOUR_PRODUCTION_CONTEXT

# Encrypt basic-auth credentials. Enter the agreed password for mvr at the prompt.
bash scripts/prod.sh seal YOUR_PRODUCTION_CONTEXT

# Log in with your usual internal registry account, then build and deploy.
docker login artifacts.r-und-t.app
bash scripts/prod.sh deploy YOUR_PRODUCTION_CONTEXT
```

The script fetches the public certificate from `sealed-secrets-controller` in
namespace `sealed-secrets`. Passwords and unencrypted Secret manifests never
reach disk. The generated `dist/herbsfest-basic-auth.sealed.yaml` is scoped
strictly to name `herbsfest-basic-auth` and namespace `rundt`.
The deploy command checks that the selected cluster can decrypt it, detects
the node architecture, builds/publishes with ko, applies `dist/prod.yaml`, and
waits for CNPG, the Deployment, and TLS. It seals credentials automatically
if the generated file is missing. Every Kubernetes operation names the context.

To prepare a complete deployment bundle for review without applying it:

```sh
make prod-resolve                      # requires the seal step and registry login
# Apple Silicon local Go builds still target production amd64 by default.
# Override if your production nodes use another architecture:
make prod-resolve PROD_PLATFORM=linux/arm64
```

The bundle includes the SealedSecret and digest-pinned images for both the app
and migration container. `make render OVERLAY=prod` only renders the static app
overlay and retains `ko://` references; it does not include generated credentials.
All generated files are under ignored `dist/`. Only the encrypted SealedSecret
belongs in Git. This setup does not yet configure CNPG backups.

After deployment, HTTP should redirect to HTTPS, and an unauthenticated HTTPS
request should return `401` with a basic-auth challenge:

```sh
curl -I http://verwaltung.ringinger-herbstfest.de/
curl -I https://verwaltung.ringinger-herbstfest.de/
curl --user mvr https://verwaltung.ringinger-herbstfest.de/ # prompts for password
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
