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
config/overlays/prod/    production namespace and CNPG connection patch
config/kind/             local cluster configuration
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
make render OVERLAY=prod
make kind-down         # deletes the local cluster and its database data
```

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

The target cluster must already have the CloudNativePG operator and its
`postgresql.cnpg.io/v1` CRDs installed, plus a default StorageClass. The prod
overlay creates a three-instance CNPG cluster with 10Gi per instance. Set a
StorageClass and adjust resources/replica counts for your cluster as needed.
CNPG creates `herbsfest-postgres-app`; the application and migration container
read its `uri` key as `DATABASE_URL`. No real passwords are stored in Git.

To build, publish images, and render deployment YAML:

```sh
export KO_DOCKER_REPO=registry.example.com/your-project
make resolve OVERLAY=prod
kubectl --context YOUR_PRODUCTION_CONTEXT apply -f dist/prod.yaml
```

`make resolve` publishes through ko and replaces both the application and
migration image references with the built digest. `make image` only builds and
publishes the image. `.ko.yaml` configures a CGO-free build on a minimal base.
There is no Ingress yet; configure your hostname, ingress controller, and TLS
when exposing the application. The application currently has no authentication.
CNPG backup configuration is also intentionally left for your storage/provider
choice before keeping important festival data.

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
