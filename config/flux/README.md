# Application inventory

`apps.yaml` is the central Flux inventory for this repository. Add each new
application here with its own overlay and readiness checks. Flux discovers
these entries automatically; there is no separate `kubectl apply` per app.

The ownership chain is:

```text
k8s-infra: rundt-herbsfest -> config/flux/apps.yaml
                             |- rundt-herbsfest-web -> config/overlays/prod
                             |- rundt-zitadel       -> config/overlays/auth-prod
                             `- rundt-openfga       -> config/overlays/openfga-prod
```

The infrastructure repository owns the `herbsfest` GitRepository source and
the parent `rundt-herbsfest` Kustomization. This repository owns the child
Kustomizations and their application resources. All children use the same
source and retain the existing namespace/operator dependencies. They do not
depend on their parent, avoiding a readiness cycle. The web child depends on
`rundt-openfga` so its API credential exists before web rollout. ZITADEL startup
does not block reconciliation of the festival app.

## Move from the direct app overlay

Publish this repository's inventory to `main` first. Then update only
`rundt-herbsfest` in `k8s-infra/flux/clusters/ovh/apps.yaml`:

```yaml
spec:
  path: ./config/flux
  prune: false
  wait: false
```

Keep its existing name, source, namespace and infrastructure dependencies.
Do not point the parent to this directory with `prune: true` during the move:
its old inventory contains the live web workloads. With pruning disabled,
these stay in place while `rundt-herbsfest-web` adopts the same resources;
their names, database, credentials and image digest stay unchanged.

The parent retains `prune: false` as the default policy for unregistering an
entire application. Removing a child entry from `apps.yaml` therefore does
not uninstall that application; explicitly delete its Flux Kustomization
when intentional. Within each application, child `prune: true` still removes
resources deleted from its overlay. Database resources retain their existing
pruning protection. See [Flux garbage collection](https://fluxcd.io/flux/components/kustomize/kustomizations/#garbage-collection).

Once both repository changes reach Flux, the parent creates the children
automatically. To trigger it from the host:

```sh
bash scripts/reconcile-gitops.sh YOUR_PRODUCTION_CONTEXT
```

This checks the parent path and reconciles it with its Git source. The parent
applies the inventory without waiting for the apps to become healthy. To
verify the workloads separately:

```sh
bash scripts/check-prod.sh YOUR_PRODUCTION_CONTEXT
bash scripts/zitadel.sh check YOUR_PRODUCTION_CONTEXT
bash scripts/openfga.sh check YOUR_PRODUCTION_CONTEXT
```

No registry credentials or Kubernetes access are needed by CI beyond its
existing image publishing setup. CI renders this inventory as well as the
development, web production, auth production and OpenFGA production overlays.
