# Identity and OpenFGA integration research

Researched 2026-10-25 for [Research identity and OpenFGA integration options](https://github.com/tjrasche/HerbsfestOS/issues/2). Repository snapshot: `3963cf43b0993a2e56b45de71db72df80ec314f5`. Product policy is the input recorded in [Define and validate OpenFGA authorization](https://github.com/tjrasche/HerbsfestOS/issues/6), not a decision made by this note.

## Findings

The required mechanisms exist in the versions declared by this repository: ZITADEL **v4.19.2** and OpenFGA **v1.21.0**. Inviting someone to initialize a ZITADEL account is different from inviting an existing account to a stand or linking it to a worker profile. OpenFGA can combine lasting stand membership, edition-specific team grants, resource inheritance, and a trusted Current Edition gate. Neither the current authentication guard nor the OpenFGA client implements those application flows yet. These are repository facts and feasibility findings; no production deployment, credentials, or live configuration were inspected. [Repository ZITADEL pin](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/config/zitadel/helmrelease.yaml), [repository OpenFGA pin](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/config/openfga/deployment.yaml), [pinned ZITADEL API](https://github.com/zitadel/zitadel/blob/v4.19.2/proto/zitadel/user/v2/user_service.proto), [pinned OpenFGA Write implementation](https://github.com/openfga/openfga/blob/v1.21.0/pkg/server/commands/write.go).

## ZITADEL onboarding and verified worker linking

### Concrete APIs

| Operation | API | Relevant behavior |
|---|---|---|
| Create an identity | `POST /v2/users/new` (`CreateUser`) | Creates a human or machine user; human creation requires `user.write`. Returns the identity ID. The older human-specific creation method is deprecated. |
| Initialize a new account | `POST /v2/users/{user_id}/invite_code` (`CreateInviteCode`) | Sends a code or returns it for caller delivery. Creating a new code invalidates its predecessor. |
| Accept account initialization | `POST /v2/users/{user_id}/invite_code/verify` (`VerifyInviteCode`) | Verifies the email and enables first authentication-method setup through the available organization methods. This response is not an application login session. |
| Reissue initialization | `CreateInviteCode` again | Prefer this to the deprecated `/invite_code/resend` method. |

Sources: [CreateUser](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.CreateUser), [CreateInviteCode](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.CreateInviteCode), [VerifyInviteCode](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.VerifyInviteCode), [ResendInviteCode](https://zitadel.com/docs/reference/api/user/zitadel.user.v2.UserService.ResendInviteCode). All four methods are present in the [v4.19.2 service definition](https://github.com/zitadel/zitadel/blob/v4.19.2/proto/zitadel/user/v2/user_service.proto).

The pinned server permits initialization invitations only when the user has no primary authentication method. A call concerning another user checks `user.write`; an authenticated API entry point does not mean arbitrary callers can initialize other identities. Consequently, **an already initialized account needs an application membership/linking invitation, not another ZITADEL initialization invitation**. [Invitation command](https://github.com/zitadel/zitadel/blob/v4.19.2/internal/command/user_v2_invite.go), [eligibility write model](https://github.com/zitadel/zitadel/blob/v4.19.2/internal/command/user_v2_invite_model.go).

The shipped invite-code generator default is **72 hours**, but this is not evidence of the deployed instance's configured lifetime. The pinned write model consumes a successful code and clears it after three failed checks or a failed expired-code check. The request contains no per-invitation expiry field. Application invitation expiry and revocation therefore need their own policy if they differ from account initialization. [Pinned defaults](https://github.com/zitadel/zitadel/blob/v4.19.2/cmd/defaults.yaml), [code lifecycle](https://github.com/zitadel/zitadel/blob/v4.19.2/internal/command/user_v2_invite_model.go), [request schema](https://github.com/zitadel/zitadel/blob/v4.19.2/proto/zitadel/user/v2/user_service.proto).

ZITADEL can deliver an invitation using its default Login UI URL or a configured URL template; supported placeholders include UserID, OrgID, and Code. Prefer the hosted flow unless the product needs a custom initialization UI. Its mail delivery still requires functioning instance delivery settings, which this research did not inspect. [SendInviteCode schema](https://github.com/zitadel/zitadel/blob/v4.19.2/proto/zitadel/user/v2/user.proto), [onboarding guide](https://zitadel.com/docs/guides/integrate/onboarding/end-users).

### Login without the club-member role

ZITADEL separates asserting role claims from requiring a role to authenticate. Project settings can reject users without any project role or reject users whose organization lacks a project grant. Those settings must permit the intended invitees; granting `club-member` to work around an upstream rejection would contradict the agreed policy. Removing the application's own `club-member` gate is a separate required change. [Project authentication settings](https://zitadel.com/docs/guides/manage/console/projects-overview).

The repository declares Login V2 and disabled self-registration as default-instance settings. Provisioning an identity and completing an invitation is therefore a concrete onboarding path to investigate, rather than assuming users can self-register. These manifests do not prove current runtime settings. A backend calling management APIs will need an appropriately scoped service identity; this note does not assume one already exists. [Repository identity configuration](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/config/zitadel/helmrelease.yaml).

### Linking identity to a pre-existing worker

The durable login identity is the validated **issuer plus subject** pair. OIDC defines `sub` as issuer-local, stable, and never reassigned; email and display name do not have that property. UserInfo's subject must match the ID Token's subject before its profile claims are used. ZITADEL exposes `email_verified` separately from `email`. [OIDC Core, subject identifiers and UserInfo validation](https://openid.net/specs/openid-connect-core-1_0.html#SubjectIDTypes), [ZITADEL claims](https://zitadel.com/docs/apis/openidoauth/claims).

Recommended application protocol, to be agreed in **Define worker profiles and invitation linking**:

1. Keep a worker's domain identity independent of any login account; an organizer can schedule an unlinked worker.
2. Store a pending application invitation bound to the particular worker and intended account or verified destination, with an unguessable single-use secret, expiry, and revocation state. ZITADEL initialization alone does not identify the domain worker to link.
3. Complete normal OIDC login. If binding to an existing ZITADEL identity, compare the validated issuer/subject with that intended identity. If binding through email proof, require the applicable verified-email evidence plus the worker-specific invitation, and resolve duplicate/shared-email cases explicitly.
4. Consume the invitation and create the identity link atomically in the application database; prevent a second acceptance or conflicting identity link. Grant permissions only through the resulting verified association.
5. Treat unlink/relink as an explicit workflow that revokes the old principal's projected access before the new association becomes usable.

This is a proposed protocol, not a provider guarantee or settled product policy. Whether to pre-create identities, support several login identities per worker, or permit shared contact addresses remains open. Never silently merge profiles by display name or email. Profile names/contact information and provider display claims can change independently; their ownership rules belong on the worker ticket.

## OpenFGA relationships and Current Edition

### Feasible representation

OpenFGA needs unambiguous object identifiers for the festival, lasting stand, edition, stand participation, and shift. This does not change the accepted Edition database key: its string-valued year label, including special labels, remains the PostgreSQL primary key. Whether an OpenFGA edition object uses that label directly or maps it to a separate identifier, and what a label correction does to that mapping, belongs to the edition and authorization tickets. Opaque OpenFGA identifiers are an option, not a new database-key requirement. [Confirmed edition constraints](https://github.com/tjrasche/HerbsfestOS/issues/5).

Represent persistent membership on the lasting stand; connect each participation to its stand and edition; place the organizer-team grant on that participation. A shift points to its participation and inherits resource permissions. OpenFGA supports these parent-child relationships and derived permissions. [Parent-child modeling](https://openfga.dev/docs/modeling/parent-child).

The following expressions are **conceptual design candidates**, not a complete or validated authorization model. Here `global` is global-organizer membership, `team` is this participation's explicit organizer grant, `member` is lasting stand membership, and `current` is a trusted gate on this participation's edition:

| Resource permission | Candidate expression preserving the confirmed baseline |
|---|---|
| View a participation's schedule | `global OR ((team OR member) AND current)` |
| Edit that schedule | `team AND (current OR global)` |
| Self-signup | `member AND current` |
| Assign another worker to a shift | Inherit participation schedule-edit permission; validate the assignment separately. |
| Manage organizer teams or activate an edition | Derive from the festival's global-organizer relation. |

Intersection and union let the final model express these restrictions together. In particular, global status does not itself confer edit permission, an old team grant does not confer historical access on a non-global organizer, and a global organizer's volunteer self-signup action remains current-edition-only. [Multiple restrictions](https://openfga.dev/docs/modeling/multiple-restrictions), [confirmed authorization policy](https://github.com/tjrasche/HerbsfestOS/issues/6).

An authenticated principal can be an opaque application principal resolved from the verified identity link. Membership may then be projected for linked workers, or represented through worker objects and their linked-identity usersets. This representation choice remains open. In either case, an unlinked worker record must never become impersonatable simply by supplying its worker ID.

### Trusted edition context

**Candidate A: a request-local gate.** Define an edition gate accepting the checked principal (or a type-bound wildcard), and supply it as a contextual tuple only for the Current Edition read from trusted server state. Contextual tuples participate in evaluation without being persisted. Membership remains on the lasting stand, so activation changes the gate rather than re-inviting every volunteer. Never construct that tuple from the browser's selected-edition field. Reserve this relation for context and exclude it from persistent tuple writes; an accidentally stored gate could otherwise keep an old edition accessible. [Contextual tuples](https://openfga.dev/docs/interacting/contextual-tuples).

**Candidate B: persist the active-edition relation.** A final model can instead derive its gate from stored activation state. Switching it then becomes a coordinated tuple update, with consistency and database synchronization consequences. **Candidate C: conditional tuples** can compare trusted request attributes with bound resource attributes, but introduce condition context and migration work. Conditions are established OpenFGA functionality; using newly experimental dynamic-condition features is unnecessary for this decision. [Conditional relationships](https://openfga.dev/docs/modeling/conditions), [v1.21.0 release](https://github.com/openfga/openfga/releases/tag/v1.21.0).

For all options, the personal selected edition identifies the requested resource and grants nothing. Load the actual resource's edition and stand from application data, then evaluate permissions. A global organizer can browse a historical edition while a volunteer self-signup check still uses the real Current Edition. Activation racing with a mutation needs an application transaction/version rule: a successful authorization response does not itself lock the Current Edition or the resource until commit. This is an architecture inference, not an OpenFGA transaction guarantee.

## Relationship writes, reads, and consistency

OpenFGA models are immutable and have explicit IDs. Pin the model ID on checks and relationship writes; using an unspecified model implicitly selects the latest. A new model does not migrate existing tuples automatically. Treat model changes, tuple migration, and application rollout as one planned transition. [Immutable models and migration](https://openfga.dev/docs/getting-started/immutable-models).

| Need | Mechanism and limit |
|---|---|
| Grant/revoke or connect resources | `POST /stores/{store_id}/write`, with `writes.tuple_keys` and/or `deletes.tuple_keys`. A request is a store-local transaction, bounded by the server's configured operation limit. |
| Retry an already completed grant/revoke | v1.21.0 supports `writes.on_duplicate: "ignore"` and `deletes.on_missing: "ignore"`. The defaults are errors. A duplicate with different condition data still conflicts. |
| Change condition data on the same tuple | Do not assume an upsert. The pinned Write validator rejects a repeated tuple key across writes/deletes in one request. Design an explicit transition rather than promising atomic replacement. |
| Inspect/reconcile direct relationships | `POST /stores/{store_id}/read`; consume continuation tokens. Read returns stored tuples and does not expand inherited permissions. |
| Decide effective permission | Check the semantic permission on the actual resource; use ListObjects/ListUsers where their query semantics fit. A successful direct Read is not a substitute for Check. |

Sources: [Write API](https://github.com/openfga/api/blob/main/openfga/v1/openfga_service.proto), [v1.21.0 validation and limits](https://github.com/openfga/openfga/blob/v1.21.0/pkg/server/commands/write.go), [v1.21.0 SQL transaction and conditional-duplicate handling](https://github.com/openfga/openfga/blob/v1.21.0/pkg/storage/sqlcommon/sqlcommon.go), [relationship queries and Read limitations](https://openfga.dev/docs/interacting/relationship-queries).

Default query consistency is `MINIMIZE_LATENCY`; cached results can miss a just-written grant or revocation. `HIGHER_CONSISTENCY` bypasses the cache and queries the datastore. This is useful after relationship changes and for revocation-sensitive checks, but it is not an atomic transaction with application state, nor a guarantee that an already-running authorized request is cancelled. Any application cache adds another invalidation responsibility. The precise acceptable revocation window remains a product/architecture decision. [Query consistency](https://openfga.dev/docs/interacting/consistency).

### Source of truth and partial failure

OpenFGA's guidance allows either storing role/grant data there or projecting data from an application source of truth. Entity hierarchy and search/filter data typically stay in the application database. Select ownership explicitly for every relationship rather than permitting two independent writers. [Source-of-truth guidance](https://openfga.dev/docs/best-practices/source-of-truth).

Recommendation for discussion: keep worker links, membership, team grants, and resource hierarchy in application-owned durable records; project their authorization representation through an outbox/reconciler. No cited API supplies a distributed transaction spanning this database, ZITADEL, and OpenFGA. Therefore:

- Record a transition as pending until the required relationship update is confirmed; do not report a grant as usable while projection failed.
- Gate affected access locally during an incomplete revocation, or choose another explicit fail-closed protocol. Merely enqueueing a delete can leave an old grant effective.
- Serialize/version changes per relationship, so retrying an old grant cannot resurrect it after a newer revoke. Idempotent duplicate handling alone does not order conflicting operations.
- Treat a timeout as an unknown write outcome. Reconcile the intended state using safe retries and direct reads; use bounded retries for transient failures, and surface invalid-model/input failures for correction.
- Deleting application objects requires planned tuple cleanup, including relationships where the deleted object appears as the tuple's subject. Do not reuse identifiers. An application-row deletion does not notify OpenFGA by itself.
- Keep actor authorization separate from the backend's ability to call Write. Check the caller's resource permission before accepting grant-management commands, including explicit organizer self-grants.

These are proposed coordination rules derived from the separate systems, not claims that an outbox alone guarantees immediate revocation. Decide the source of truth, pending-state behavior, bootstrap owner, and reconciliation policy in the authorization ticket.

## Repository integration gaps

The snapshot's [authentication handler](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/internal/auth/handler.go) protects private routes using a `Member` value captured at login from `club-member` claims. Its [session user](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/internal/auth/session.go) stores subject/name/email, not verified-email evidence or a worker link. Invitation and worker-link persistence do not exist in the inspected application packages.

The [OpenFGA client](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/internal/openfga/client.go) has Ping and Check, requires a configured model for Check, and distinguishes denial from operational failure. Check has no contextual-tuples, conditions context, or consistency input. There are no tuple-read/write, pagination, model-installation, or reconciliation operations. The shared HTTP helper accepts only HTTP 200 and caps response bodies at 64 KiB, which future operations must account for. [Application construction](https://github.com/tjrasche/HerbsfestOS/blob/3963cf43b0993a2e56b45de71db72df80ec314f5/cmd/web/main.go) creates the client but does not inject it into protected handlers.

The eventual implementation therefore needs authenticated identity resolution, worker-link and invitation workflows, semantic resource authorization, an edition-context boundary, and a relationship-management path with explicit failure handling. Preserve the client's denial/error distinction: unavailable or unconfigured OpenFGA must not become permission to access newly protected resources. Existing-route admission and migration remain the subject of their own decision ticket.

## Decisions still belonging to the map

- **Define worker profiles and invitation linking:** profile fields; duplicates/shared addresses; proof and conflict handling; application invitation expiry/reissue; link cardinality; unlink/relink authority; directory disclosure and volunteer profile visibility.
- **Define edition and stand lifecycle:** activation timing and concurrent-request semantics; participation removal; copying organizer-team grants; edition-label corrections and OpenFGA object-ID mapping. Edition start/end days are now confirmed domain data; their storage placement must be resolved against the accepted one-column Edition-table constraint in that ticket.
- **Define and validate OpenFGA authorization:** exact model and principal representation; trusted Current Edition mechanism; source of truth; grant/revoke completion behavior; bootstrap and reconciliation; field-level directory permissions.
- **Define shifts and assignment rules:** capacity, withdrawals, membership-revocation effects on existing assignments, and transaction rules. OpenFGA grants permission to request an assignment; it does not reserve capacity.

Before approving the final model, validate global viewing without editing, explicit self-grants, own/other stands, old/future team grants, activation with lasting membership, absent participation, forged edition input, revoked/unlinked identities, directory restrictions, and authorization-service failure. OpenFGA provides model-test tooling, but this research did not install a model or execute those acceptance checks. [Model testing](https://openfga.dev/docs/modeling/testing).
