# HerbstfestOS

Barebones server-rendered Go application for managing a nonprofit festival.
The first domain is a feedback note: a specific feedback point to track.
Signed-in festival team members create and list notes at `/`; htmx updates
the page without a full reload, and the form also works with JavaScript
disabled. `/public/` is an anonymous landing page.

## Stack and structure

- Go 1.26+, standard `net/http` routing and cross-origin protection.
- templ for HTML, self-hosted htmx 2.0.11, plain CSS, embedded assets.
- GORM with its PostgreSQL driver.
- ZITADEL OIDC with PKCE for production login and project-role authorization.
- OpenFGA infrastructure and a standard-library Go client for future permissions.
- ko for container builds; Kustomize for Kubernetes configuration.

```text
cmd/web/                   server, dependency wiring, public/private routing, migrations
internal/feedbacknote/     entity, service, repository, handler, templates
internal/database/         database connection and pool
internal/auth/             OIDC login, encrypted sessions and project-role guard
internal/openfga/          reusable REST client, configuration and connectivity check
internal/ui/               shared layout, public landing page and embedded assets
config/app/                application Deployment, Service, configuration
config/postgres/           plain development Postgres StatefulSet and storage
config/cnpg/               production CloudNativePG Cluster
config/overlays/dev/       kind development namespace, credentials and auth bypass
config/overlays/prod/      web ingress, sealed session key, CNPG and deployment patches
config/overlays/auth-prod/ ZITADEL identity provider and its database
config/overlays/openfga-prod/ internal OpenFGA API, sealed credential and CNPG
config/openfga/            shared OpenFGA Deployment, migrations and Service
config/flux/               central Flux app inventory (web, identity and permissions)
config/zitadel/            pinned ZITADEL Helm release, ingress and sealed bootstrap
config/kind/               local cluster configuration
docs/zitadel-app-setup.txt console setup for the application and club-member role
scripts/                   host checks, reconciliation, sealing and CI publishing
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
generates templates, builds the application with ko, loads the application,
Postgres and OpenFGA images directly into kind, applies the dev overlay, and waits for the workloads. No registry or
Dockerfile is required. The build targets the kind node's architecture, including
Apple Silicon. Dev Postgres uses a 1Gi persistent volume and deliberately
public development credentials. kind's default storage provisioner handles it.
Postgres is exported as an archive containing only the node's platform, which
also works with Docker's containerd image store.

Kind deploys the Go app, OpenFGA, and separate plain Postgres instances for each. It sets
`AUTH_MODE=development`: login and role checks are bypassed, and the private
layout displays “Lokale Entwicklung”. OpenFGA's dev API uses no authentication
and is internal to the cluster; its database has a separate 1Gi volume and
public development credentials. ZITADEL is not deployed into kind.
Full SSO in the kind Deployment is not configured; see the local OIDC
instructions below to test the Go app against the production identity provider.

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
In OIDC mode, the top bar shows the verified user's name and a POST logout
button. A signed-in user without the required project role sees a friendly
403 page with instructions to contact the organizers and sign in again.

Shared templ components live in `internal/ui/components.templ`; icons live in
`internal/ui/icons.templ`. Wrap a domain page in `ui.Layout(title, activePage)`
and keep its content/htmx fragments in the domain package. The feedback page
demonstrates this: htmx swaps `#notes`, while the navigation and layout remain
outside the fragment. Buttons accept templ attributes for native form behavior.
Forms remain usable without JavaScript.

The footer thanks Rasche & Thalhofer UG (haftungsbeschränkt) for providing free
hosting. Its locally embedded logo comes from
[rasche-thalhofer.cloud](https://rasche-thalhofer.cloud/assets/logo.svg.svg),
and the credit uses that website's blue palette. The acknowledgement reads
“Mit freundlicher Unterstützung durch” and “Cloud geschenkt, damit ihr feiern
könnt.” to make the hosting donation clear.

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
`make run` and the kind overlay explicitly set `AUTH_MODE=development`, which
bypasses SSO and labels the session as local development. Running the binary
without that setting defaults to OIDC and fails startup if configuration is missing.

## Routes and authentication

The production app is served at <https://herbstfest.r-und-t.app>. ZITADEL's
issuer is <https://auth.r-und-t.app>; its administrator console is at
<https://auth.r-und-t.app/ui/console/>.

| Method | Route | Access and behavior |
| --- | --- | --- |
| GET | `/` | Feedback notes; requires the project role `club-member` |
| POST | `/feedback-notes` | Creates a note; requires the same role |
| GET | `/design-system` | Component gallery; requires the same role |
| GET | `/public` | Anonymous redirect to `/public/` |
| GET | `/public/` | Anonymous festival landing page |
| GET | `/static/...` | Public embedded CSS, JavaScript, logos and fonts |
| GET | `/auth/login` | Starts a ZITADEL login |
| GET | `/auth/callback` | Validates the OIDC response and creates the app session |
| POST | `/auth/logout` | Clears the app cookie and redirects to ZITADEL logout |
| GET | `/healthz` | Public process liveness probe |
| GET | `/readyz` | Public database readiness probe |

`cmd/web/routes.go` mounts a separate public mux under `/public/`, outside the
auth middleware. Public handlers register relative paths: `/signup` on that
mux would become `/public/signup`. Register each future public domain handler
there; keep private handlers on the protected mux in `cmd/web/main.go`. The
initial public template is `internal/ui/public.templ` and reuses the design
system components. Unknown public subpaths return 404 without starting login;
paths such as `/publicity` remain protected. Cross-origin protection still
applies to the whole server, including future public forms.

### ZITADEL project and membership

The app uses the official ZITADEL OIDC library with Authorization Code + PKCE,
state and nonce validation, and verified ID tokens/userinfo. The Web/PKCE
client has no client secret. Its configured public identifiers are:

| Setting | Value |
| --- | --- |
| Project ID | `394051632024780961` |
| Production client ID | `394051797699788961` |
| Issuer | `https://auth.r-und-t.app` |
| Redirect URI | `https://herbstfest.r-und-t.app/auth/callback` |
| Post-logout redirect URI | `https://herbstfest.r-und-t.app/` |
| Required project role | `club-member` |

Project and application configuration is managed in the ZITADEL console.
Enable **Assert Roles on Authentication**, create the `club-member` role,
and explicitly assign it to each festival team member through the project's
authorizations. Organization/instance administrator status alone does not
grant app access. The app checks the project-specific roles claim; an
identically named role in another project does not grant access. See the
[console setup guide](docs/zitadel-app-setup.txt) for the detailed steps.

Unauthenticated private requests return `303` to `/auth/login`. Expired htmx
sessions return `401` with `HX-Redirect` so the whole browser goes to login.
Authenticated users without `club-member` receive `403`; htmx redirects them
to the full access-denied page. Successful login always returns to `/`.

### Configuration and sessions

Environment variables are read directly; `.env.example` is a reference, not
an automatically loaded configuration file.

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL connection string |
| `HTTP_ADDR` | Listen address; defaults to `:8080` |
| `AUTH_MODE` | Defaults to `oidc`; `development` explicitly bypasses auth |
| `ZITADEL_ISSUER` | OIDC issuer origin |
| `APP_BASE_URL` | Browser-facing app origin, used for callback/logout URLs |
| `ZITADEL_CLIENT_ID` | Web/PKCE application client ID |
| `ZITADEL_PROJECT_ID` | Project whose `club-member` role grants access |
| `AUTH_SESSION_KEY` | Base64-encoded random 32-byte app session key |

OIDC mode fails startup if required auth configuration is missing. Origins
must use HTTPS; HTTP is allowed only on localhost/loopback for local tests.
The migration command exits before initializing authentication.

The production ConfigMap contains the public OIDC settings. The separately
sealed `rundt/herbsfest-auth` Secret supplies `AUTH_SESSION_KEY` to the web
container only. This key is separate from ZITADEL's own secrets and shared by
app replicas. AES-GCM encrypts and authenticates the host-only, Secure,
HttpOnly, SameSite=Lax session cookie; local HTTP uses a non-Secure cookie.
Sessions last at most one hour and never outlive the ID token. Role changes
take effect at the next login or session expiry. Rotating the session key
invalidates existing app sessions.

### Test SSO locally

Create a separate Web/PKCE client in the same project with these settings:

- Redirect URI: `http://localhost:8080/auth/callback`.
- Post-logout redirect URI: `http://localhost:8080/`.
- Development mode enabled for this local client only.

Keep the production client's development mode disabled. With kind's Postgres
port-forwarded as described above, run the Go app directly:

```sh
export DATABASE_URL='postgres://herbsfest:herbsfest@localhost:5432/herbsfest?sslmode=disable'
export AUTH_MODE=oidc
export ZITADEL_ISSUER=https://auth.r-und-t.app
export APP_BASE_URL=http://localhost:8080
export ZITADEL_CLIENT_ID=YOUR_LOCAL_CLIENT_ID
export ZITADEL_PROJECT_ID=394051632024780961
export AUTH_SESSION_KEY="$(openssl rand -base64 32)"
make run
```

Use your local client ID and a user assigned `club-member` in the project.
`make run` respects the explicit OIDC mode. These shell exports configure the
local Go process; they do not change the kind Deployment. To run SSO inside
kind, its dev ConfigMap would need OIDC settings and its Deployment a local
session-key Secret reference; those are not part of the default dev overlay.

## OpenFGA connection scaffold

OpenFGA is installed as a future authorization backend. The existing ZITADEL
`club-member` guard continues to protect private routes, and `/public/` stays
anonymous. No authorization model, store, relationship tuples or application
service decorators are installed yet. Missing store/model configuration makes
`Client.Check` return an error; it never grants access by default.

`internal/openfga` uses standard `net/http` without an additional SDK dependency.
`cmd/web` constructs one reusable client when `OPENFGA_API_URL` is configured,
with bounded requests, context cancellation and redirects disabled. Constructing
the client makes no network calls and does not make web startup or `/readyz`
depend on OpenFGA while routes do not use it. Database migrations skip OpenFGA
initialization entirely. A standalone local run may omit all OpenFGA settings.

| Variable | Purpose |
| --- | --- |
| `OPENFGA_API_URL` | Internal HTTP(S) API origin; unset disables the connection scaffold |
| `OPENFGA_API_TOKEN` | Backend API credential; required by the production server, omitted in dev |
| `OPENFGA_STORE_ID` | Store ULID; configure together with the model ID when introducing policy |
| `OPENFGA_AUTHORIZATION_MODEL_ID` | Explicit model ULID; no implicit latest-model fallback |
| `OPENFGA_TIMEOUT` | Request timeout; defaults to `2s`, accepts `1ms` through `30s` |

`Client.Ping(ctx)` makes an authenticated, read-only List Stores request and
requires no store/model configuration. `Client.Check(ctx, user, relation,
object)` returns `(false, nil)` for a denial and `(false, err)` for missing
configuration, invalid requests/responses, failed API authentication or
unavailable service. Future domain authorization adapters should map verified
ZITADEL subjects to user IDs and semantic permissions to these checks.

To verify the Go client against the kind API, run this in one terminal:

```sh
make dev-openfga-forward
```

In another terminal:

```sh
OPENFGA_API_URL=http://localhost:8180 go run ./cmd/web -check-openfga
```

The command exits after verifying API connectivity; it needs no application
database or ZITADEL configuration. The same port forward can be used by a
local Go process running with `make run`. Configure its URL explicitly; the
kind Deployment uses `http://openfga:8080` automatically.

The server image is pinned to
[OpenFGA v1.21.0](https://github.com/openfga/openfga/releases/tag/v1.21.0).
A same-image init container runs `openfga migrate` before the API starts.
One replica with `Recreate` avoids running the old API alongside migrations
for a new version, at the cost of downtime during upgrades. The ClusterIP
Service exposes only HTTP; gRPC listens on pod loopback, and the playground
and metrics listener are disabled. Server readiness checks datastore health;
liveness checks the HTTP listener.

Production has a dedicated CNPG database `openfga-postgres`, one PostgreSQL
17.11 instance and 5Gi Longhorn storage. OpenFGA reads the CNPG-generated
username/password separately and verifies database TLS using the projected
public CA certificate. The database is protected from Flux pruning; high
availability and backups are not configured. The API uses a random preshared
key in `rundt/openfga-api-auth`, sealed strictly for that namespace/name. Only
the OpenFGA server and the web container receive this key; neither application's
database migration container needs it. API traffic remains internal HTTP;
there is no ingress or public hostname.

After publishing the manifests, check production from the host:

```sh
bash scripts/openfga.sh check default
```

This reconciles the inventory and `rundt-openfga`, waits for CNPG and the
Deployment, then verifies datastore health, authenticated API access and
rejection of anonymous API requests through a temporary loopback port forward.
It requires `kubectl`, `flux` and Python 3. Credentials are held in memory
and never printed or written to disk. For a persistent production port forward:

```sh
bash scripts/openfga.sh forward default
```

This forwards `localhost:8180`; API calls still require the production key.
Render the separate production resources with
`make render OVERLAY=openfga-prod`. See the
[OpenFGA configuration guide](https://openfga.dev/docs/getting-started/setup-openfga/configure-openfga)
for migrations, API authentication and health semantics.

## Production configuration

The prod overlay follows the existing apps in
[k8s-infra](https://github.com/rasche-thalhofer/k8s-infra/):

- Existing namespace `rundt` and image pull Secret `oci-registry-cred`.
- Image repository `artifacts.r-und-t.app/herbsfest`, built by ko.
- Traefik serves `https://herbstfest.r-und-t.app`, redirects HTTP to HTTPS,
  and sends `Cache-Control: private, no-store`.
- cert-manager uses `bunnycdn-issuer` for `herbsfest-tls` and `zitadel-tls`.
  The configured app and auth hostnames are `herbstfest.r-und-t.app` and
  `auth.r-und-t.app`, using the existing `*.r-und-t.app` DNS setup.
- The app uses one CNPG instance with 5Gi Longhorn storage. CNPG creates
  `herbsfest-postgres-app`; the web and migration containers read its `uri`
  key. This is not an HA database setup.

The cluster needs Traefik, cert-manager, CloudNativePG, Sealed Secrets, and
Longhorn installed by the infrastructure repository. The overlay reuses its
namespace and registry Secret; it does not take ownership of them.

ZITADEL is deployed independently through `config/overlays/auth-prod`: the
official Helm chart is pinned to `10.3.0`, with API and Login V2 images pinned
to `v4.19.2`. It uses one API replica, one Login UI replica, and its own
PostgreSQL 17.11 CNPG instance with 5Gi Longhorn storage. Traefik terminates
HTTPS and forwards h2c to the API on port 8080; `/ui/v2/login` routes to the
Login UI on port 3000. The chart manages the Login UI system user and keys.

`rundt/zitadel-bootstrap` is sealed separately and contains the masterkey,
initial administrator password and Login UI cookie secret. The bootstrap
administrator is `t@rasche-thalhofer.cloud`, with a required password change
on first login. Self-registration is disabled in the bootstrap configuration.
These bootstrap settings do not update existing accounts or policies when
edited later; manage those through the console. See the
[ZITADEL infrastructure guide](config/zitadel/README.md) for secrets, database
TLS, chart operations and recovery. All three databases have Flux pruning disabled;
backups and high availability are not configured.

### Basic Auth retirement

Production ingress now uses app-level ZITADEL SSO. Basic Auth has been removed
from its middleware chain, allowing `/public/` and the login endpoints to be
accessed anonymously. The private-cache middleware remains attached.

The Basic Auth middleware and sealed Secret remain as inactive rollback
resources. Reattach `rundt-herbsfest-basic-auth@kubernetescrd` before reverting
to an image without SSO; doing so also protects public routes.
`scripts/enable-sso.sh` is the host-side cutover check: it waits for rollout,
verifies private-route guards and the production PKCE configuration through
a temporary port forward, then removes Basic Auth from the local ingress
file if still present. With the current ingress, it verifies the deployment
without changing the file. It neither applies cluster changes nor publishes
commits. The normal SSO cutover has already been completed.

### GitOps deployment

This repository owns the production app manifests, including the sealed
session key and inactive Basic Auth Secret, in `config/overlays/prod`.
The infrastructure repository registers the public app repository as Flux source `herbsfest`; the existing OVH
Kustomization `rundt-herbsfest` reads the central inventory in
`config/flux/apps.yaml`. It automatically creates `rundt-herbsfest-web` for the
production web overlay, `rundt-zitadel` for the auth overlay and `rundt-openfga`
for the permissions backend. The web child depends on OpenFGA being ready so
its API Secret exists before web rollout. The ownership chain is:

```text
k8s-infra: GitRepository herbsfest (this repository's main branch)
           rundt-herbsfest -> ./config/flux/apps.yaml
                               |- rundt-herbsfest-web -> ./config/overlays/prod
                               |- rundt-zitadel       -> ./config/overlays/auth-prod
                               `- rundt-openfga       -> ./config/overlays/openfga-prod
```

All these Flux objects live in namespace `flux-system`. The exact parent name
is `rundt-herbsfest`, not `rundt-herbstfest`. The cluster-wide bootstrap
Kustomization `flux-system` retains its existing name and belongs to infra.

The parent applies the inventory with `wait: false` and `prune: false`; each
child prunes its own application resources and has its own readiness checks.
Removing a child entry does not automatically uninstall it. The database
Clusters retain explicit pruning protection. The children have infrastructure
dependencies, without depending on their parent and creating a readiness cycle.

The shared namespace, registry pull Secret, and cluster operators stay in
infra. See [the inventory guide](config/flux/README.md) for the one-time
infrastructure entrypoint change and host reconciliation command.

Every push to `main` runs `.github/workflows/ci.yml`:

1. Generate templates, run Go tests and vet, check generated files are committed,
   test the image-digest publisher, and render the dev, web prod, auth prod,
   OpenFGA prod and central Flux overlays.
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

Builds queue without cancellation. Each revision publishes an image, but only a build whose source SHA still matches
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

Run these from the repository on a host with your kubeconfig available. The
examples use context `default`; replace it if your production context differs.
The scripts always use the supplied context explicitly.

```sh
# Reconcile the source and central application inventory.
bash scripts/reconcile-gitops.sh default

# Reconcile and check the app database, Deployment and TLS certificate.
bash scripts/check-prod.sh default

# Reconcile and check ZITADEL's database, Helm release, API/Login UI and TLS.
bash scripts/zitadel.sh check default

# Reconcile and check OpenFGA's database, Deployment and API authentication.
bash scripts/openfga.sh check default

# Verify the deployed app's guards and PKCE settings; Basic Auth is already off.
bash scripts/enable-sso.sh default
```

The checks require `flux` and `kubectl`; the ZITADEL check also uses `curl`.
The cutover check uses `kubectl`, `curl` and Python 3. The source and parent
inventory must have been registered by infra first. For direct Flux commands:

```sh
flux --context default reconcile kustomization rundt-herbsfest -n flux-system --with-source
flux --context default reconcile kustomization rundt-herbsfest-web -n flux-system
flux --context default reconcile kustomization rundt-zitadel -n flux-system
flux --context default reconcile kustomization rundt-openfga -n flux-system
```

All database checks use the fully qualified `clusters.postgresql.cnpg.io`
resource. Plain `cluster/NAME` can select the OVH operator's
`clusters.kube.ovh.edixos.io` and report NotFound for a database that exists.
To inspect the correct resources:

```sh
kubectl --context default -n rundt get clusters.postgresql.cnpg.io herbsfest-postgres zitadel-postgres openfga-postgres
```

To retrieve the initial ZITADEL administrator password privately on the host
(requires `kubectl` and Python 3):

```sh
bash scripts/zitadel.sh password default
```

This prints only the bootstrap password. After changing it in the console,
the Secret does not reflect the current account password. To reseal the
existing bootstrap values with the cluster's current public certificate
(requires `kubeseal`):

```sh
bash scripts/zitadel.sh reseal default
```

Resealing preserves the masterkey and writes only ciphertext. Commit the
updated `config/zitadel/sealedsecret-bootstrap.yaml`. Preserve the masterkey
for the lifetime of the identity database and store recovery material
securely alongside database and Sealed Secrets controller backups.

To rotate the fallback Basic Auth credentials, install `kubeseal` and Apache `htpasswd`.
On macOS, `brew install kubeseal httpd` provides them; add
`$(brew --prefix httpd)/bin` to `PATH` if needed. Run on your host:

```sh
bash scripts/prod.sh cert default
bash scripts/prod.sh seal default
```

The seal command prompts for the password for `mvr`, fetches the public
certificate from `sealed-secrets-controller` in namespace `sealed-secrets`,
and validates the result against that cluster. It updates
`config/overlays/prod/sealedsecret-basic-auth.yaml`; commit that encrypted file
to deploy the new credentials. Plain passwords and unencrypted Secret manifests
never reach disk. The ciphertext is scoped to `rundt/herbsfest-basic-auth`.

`make render OVERLAY=prod` renders the complete app configuration, including the
committed digest and SealedSecret. Development still uses ko to build local images.

Expected browser-facing behavior after the corresponding app image rolls out:

```sh
curl -I http://herbstfest.r-und-t.app/              # redirect to HTTPS
curl -I https://herbstfest.r-und-t.app/             # 303 to /auth/login
curl -I https://herbstfest.r-und-t.app/auth/login   # 302 to ZITADEL
curl -I https://herbstfest.r-und-t.app/public/      # 200 without login
curl -I https://herbstfest.r-und-t.app/public/missing # 404 without login
curl --fail https://auth.r-und-t.app/.well-known/openid-configuration
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
- [ZITADEL application console setup](docs/zitadel-app-setup.txt)
- [ZITADEL Kubernetes operations and bootstrap secrets](config/zitadel/README.md)
- [Flux application inventory and ownership](config/flux/README.md)

## Volunteer scheduling MVP

Scheduling now owns `/`, `/admin`, `/directory`, `/workers/{id}`, `/print/{participation}`, and edition-scoped `/feedback-notes`. Public names-only signup is `/register/{token}`. Private account connections and organizer invitation acceptance use `/proof/{token}`. The existing shell and German branding remain. PostgreSQL is authoritative for identities, grants, memberships and lifecycle gates; a pinned OpenFGA model checks contextual facts from the locked transaction. No identity-provider display field or browser edition selection grants access.

### Disposable local rehearsal

Requirements: Go 1.26+, PostgreSQL (tested here with 18.6), OpenFGA v1.21.0, and Python Playwright/Chromium for browser checks. Bind development services to loopback. Use **new disposable databases**; these commands are not a production cutover.

```sh
# Start your disposable PostgreSQL on 127.0.0.1:55432 first.
createdb -h 127.0.0.1 -p 55432 scheduling_rehearsal
createdb -h 127.0.0.1 -p 55432 scheduling_test
openfga run --datastore-engine=memory --http-addr=127.0.0.1:58180 \
  --grpc-addr=127.0.0.1:58181 --playground-enabled=false --metrics-enabled=false
```

In another terminal, from the repository root:

```sh
python3 scripts/local-scheduling-model.py > /tmp/herbstfest-model.env
. /tmp/herbstfest-model.env
export DATABASE_URL='postgres://localhost:55432/scheduling_rehearsal?sslmode=disable'
# Add your local database username to that URL if necessary.
go run ./cmd/rehearsal
# Loads only the approved synthetic 10-stand / 250-worker / 150-shift fixture.
# Refuses a non-loopback target, a database without the _rehearsal suffix,
# or a target already containing editions.
go tool templ generate
AUTH_MODE=development HTTP_ADDR=127.0.0.1:58080 go run ./cmd/web
```

Open `http://127.0.0.1:58080/`. The rehearsal explicitly grants the synthetic local identity `urn:herbstfest:local` / `developer` global and team access. Signup is `http://127.0.0.1:58080/register/synthetic-rehearsal-registration-only`. Development authentication still goes through database/OpenFGA permission checks. Guest signup stays anonymous in this mode. Optional hosted login requires a separately configured development ZITADEL client; the rehearsal does not contact or modify the live provider.

```sh
SCHEDULING_TEST_DATABASE_URL='postgres://localhost:55432/scheduling_test?sslmode=disable' \
SCHEDULING_TEST_FGA_URL=http://127.0.0.1:58180 go test ./...
go vet ./...
# With Playwright/Chromium installed in your selected Python environment:
python3 scripts/test-scheduling-browser.py
```

Integration tests isolate each case in a new PostgreSQL schema and fresh in-memory FGA store, then drop the schemas. Never point the integration suite at production. Without both test environment variables, database integration tests explicitly skip. Browser artifacts go to `/tmp/herbstfest-browser` by default. The browser test submits synthetic requests and requires the local rehearsal application.

For this SSH host, tools were installed without sudo under `/tmp/herbstfest-tools`; Go is `/tmp/herbstfest-tools/go/bin/go`, PostgreSQL binaries are under `/tmp/herbstfest-tools/pg/usr/lib/postgresql/18/bin`, and `LD_LIBRARY_PATH=/tmp/herbstfest-tools/pg/usr/lib/x86_64-linux-gnu` is required for PostgreSQL client tools. Existing disposable OpenFGA is `/tmp/wayfinder-validation/openfga`; browser Python is `/tmp/wayfinder-validation/browser-env/bin/python`. The local database user is `tjark`. These temporary paths are session conveniences, not deployment dependencies.

### GitOps deployment and initial setup

CI publishes one image digest for the web and `prepare` init containers. Flux
reconciles the Deployment using Kubernetes' default rolling-update strategy.
The new web process starts after preparation succeeds; the old pod may briefly
return errors while migrations run. This tradeoff is accepted during development.
No workstation Kubernetes access or manual in-cluster commands are required.

The init container runs `web -prepare`. It applies the versioned transactional
migration, provisions the compiled OpenFGA policy in a dedicated empty store,
and records exact immutable store/model IDs plus the compiled model hash in
PostgreSQL. Repeated reconciliation verifies and reuses that committed pin;
normal web startup only reads it (`OPENFGA_MANAGED_MODEL=true`). It never selects
an implicit latest model. A changed policy gets its own binding. Failed network
outcomes may leave an unreferenced store, but never publish a partial binding.
Persisted grant tuples in the contextual-only store are rejected during preparation.

The containers use the existing CNPG and OpenFGA Secret references. No cluster
credentials or API tokens are copied into Git, CI, or this host. GitOps has no
Kubernetes API write permissions in the application pod.

Set initial non-secret inputs in
`config/overlays/prod/scheduling-setup.env`, then commit and push:

- If unscoped legacy feedback exists, supply the explicitly selected
  `LEGACY_EDITION_LABEL`, `LEGACY_START_DAY`, and `LEGACY_END_DAY`. Empty databases
  do not need them. Missing required legacy inputs stop preparation before writes;
  no dates or production rows are guessed, deleted, or reassigned on reruns.
- Set `BOOTSTRAP_ISSUER` and `BOOTSTRAP_SUBJECT` together to identify the initial
  organizer from validated OIDC identity. Neither email nor provider display names
  establish ownership. Empty bootstrap inputs grant nobody access. The bootstrap
  is recorded once, so later grant revocation survives reconciliation.

Kustomize hashes the setup ConfigMap, so changing these inputs rolls the pod
and reruns preparation. Missing/invalid inputs or an unavailable authorization
engine keep the new web pod stopped. Check GitOps status/logs through the existing
operations workflow. The command-line `-migrate` and `-bootstrap-organizer` entrypoints
remain available for deliberate maintenance; they are not prerequisites for GitOps.

For a deployment with meaningful data, test backup/restore and retain the maintenance
window before migrating. Never downgrade to the old unscoped feedback binary after
scoped writes; recover before new writes or use a forward fix. Hosted ZITADEL
registration/provider settings remain independent of application deployment.

Operator-only ownership maintenance must acquire
`SELECT revision FROM scheduling_state WHERE id=1 FOR UPDATE`, perform changes,
increment `revision`, and commit together. Application transactions acquire this
revision lock before resource locks. Never cache grants or worker bindings in sessions.

### Verification and release boundary

Application integration coverage includes competing approvals, atomic batch failure, duplicate prevention/retries, fresh submission reuse, one-to-one link conflicts/replacement/expiry, concurrent invitation redemption, archived own requests, next-request operator unlink/team revocation, authorization snapshots racing with revocation, edition copies/date corrections, cancellation/membership preservation, feedback isolation, transactional migration failure/rollback/rerun, and authorization outages. Unit checks cover the Berlin calendar window; existing OIDC nonce/state/PKCE tests remain. Browser checks exercise the 150-shift desktop shell, names-only phone submission, privacy of public pages, lack of horizontal overflow, and confirmed-roster print output. These are automated implementation checks, not live-user acceptance.

The future human one-stand pilot has **not** occurred. Before broader invitations, perform the approved pilot with a few workers: signup, optional hosted account connection, approval, printed delivery, revocation, and organizer feedback. Organizers communicate assignment changes manually. There is no automatic scheduling, bulk roster import, or assignment email/SMS delivery.

Session verification (2026-10-29): `go test ./...` passed with both integration environment variables set; `go vet ./...` and `git diff --check` passed. Chromium passed desktop 10-stand/150-shift rendering, 390px signup submission, print-PDF output and public-navigation privacy without JavaScript errors. Screenshots were visually inspected. A custom-format PostgreSQL backup was restored into another disposable database and counts checked (10 stands, 150 shifts, 252 workers after two synthetic browser/API submissions). Go's `-race` instrumentation was attempted but unavailable because this host has no C compiler; the database concurrency tests themselves ran successfully. No live pilot, deployment, commit, push, production migration, invitation delivery, or live IdP change occurred.
