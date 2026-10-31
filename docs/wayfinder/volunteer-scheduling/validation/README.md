# Scheduling validation handoff

These are throwaway planning artifacts, not application implementation. No production database, identity provider, OpenFGA store, routes, or access gates are changed. Do not promote prototype JavaScript into production.

## Preview

From the repository root:

```sh
python3 scripts/serve-scheduling-prototype.py
```

Open <http://127.0.0.1:8090/prototype/volunteer-scheduling?variant=timeline>. Use `variant=table` or `variant=inbox`, the bottom switcher, or arrow keys outside input controls. For SSH preview, forward port 8090 from this host. The server binds loopback only. Existing static brand assets are served from the repository; no package installation or database is needed.

All state is in the current tab, resets on reload, and is shown in the expandable state panel. Roles are simulation controls, not security boundaries. There are no real invitations, identities, account connections, grants, or submissions. The two festival editions have separate example schedules. A second participating stand is assumed when disabling the displayed stand. Global activation checks and dates are illustrative, not a complete lifecycle implementation. Expiry, invitation consumption, worker merging, and true database races require application validation later.

Review these three structurally different organizer views:

1. Timeline: compare staffing through the day.
2. Table: compare dense scheduling and roster detail.
3. Inbox: compare approval-first work with a staffing sidebar.

Review guest signup at 390px width: names only, select shifts, save all requests together, then optionally simulate linking an account. The race button fills a selected shift without discarding the form; submitting should preserve input and save nothing. Switch to the organizer to approve a request. Global visibility alone cannot edit; use the explicit self-grant control. Test an old edition and disabled participation. A worker sees their own archived request without withdrawal. Check the print view: only confirmed names and shifts, edition and print timestamp.

**Human review accepted:** the user confirmed the prototype is sufficient for the MVP. The role dropdown is demo-only; production retains the existing sidebar/header around the main scheduling view. Use the default timeline initially; the alternatives remain prototype exploration. Live-pilot validation is still pending. No branch/commit has been published: explicit commit/publish authorization is still required for capturing this prototype on a throwaway branch.

## Authorization model

`authorization-model.json` is an OpenFGA schema 1.1 model. The application database is authoritative. For the MVP, build bounded contextual facts for the authenticated principal, actual resource, current worker connection, relevant memberships, teams, current edition, participation state, and validated registration capability. The proof-of-concept supplies hierarchy facts contextually too; persisting stable hierarchy is optional and unnecessary for this validation. Never persist live grants or context gates alongside this design: forgotten stored tuples would defeat immediate revocation.

Use stable opaque IDs for authorization resources, mapped separately from the editable Edition string key. Model IDs must be pinned. Evaluate semantic permissions, rather than role-name conditions scattered through handlers. Global administration actions use festival permissions. Directory lookup requires a separate authorized organizer lookup operation; nonmember fields remain masked unless the worker detail permission succeeds. Guest capabilities reveal only permitted signup information; never call roster permissions to render public signup.

The model implements global schedule visibility; edit requires an explicit team grant, enabled participation, and either Current Edition or global access. Worker-private access uses fresh owner facts; old or disabled own requests remain readable but cannot be withdrawn. Request status, shift start/cancellation/capacity, dates, capability expiry, and field masking remain domain checks, not facts a browser may assert. Pending status is required before withdrawal or approval.

For every operation load a consistent DB snapshot, construct context from that snapshot and actual resource ownership, perform the OpenFGA check, and protect the mutation commit against relevant grant/link/context changes. Serialize with a shared authorization revision row (take the strongest required lock before other locks or authorization; assignment actions that may establish membership take the exclusive lock): permission-sensitive mutations hold a shared lock through authorization and commit; grant/link/membership/activation changes take an exclusive lock and increment the revision. An operation that began earlier may finish before a removal; no newly authorized request after the removal may use stale grants. Operator-only ownership changes must use the same locking maintenance procedure. Never keep an allow-result or worker binding in the login cookie.

Assignment and membership creation commit in one database transaction. Lock the shift for capacity changes and approvals; unique worker/shift assignment constraints and idempotency prevent duplicates. Guest multi-shift submission validates all chosen shifts under locks in deterministic ID order before writing the worker and requests. This describes required application behavior; no application transactions are implemented in this packet.

Context is limited to 100 tuples per check. Query only paths relevant to the principal and target; do not send the entire festival graph. Add a bound check before each request and fail closed on oversized/incomplete context. The rehearsal must include a worker spanning all ten stands. If paths cannot fit, split independent OR-path checks or introduce persisted immutable hierarchy with integration coverage; never silently truncate facts. No client-selected edition, arbitrary worker ID, email/name match, or role-looking claim may create a contextual grant.

On OpenFGA configuration/transport failure, return a retryable unavailable response and show no protected data. Keep public landing, design-system examples, authentication, static assets, and operational endpoints available. Protect signup submissions too. Hosted ZITADEL registration remains email/social; disable automatic provider email/name linking and allow manual provider linking only after authenticating the existing identity. Names/contact data never overwrite identity ownership.

Run actual engine checks against a downloaded, checksum-verified OpenFGA v1.21.0 binary:

```sh
python3 scripts/validate-scheduling-authorization.py --binary /path/to/openfga
```

The runner starts an isolated in-memory server on loopback, installs the model in a new throwaway store, checks permissions, and terminates it. It does not read app environment credentials or contact configured services. Engine assertions do not prove future app context construction, database transaction correctness, migration safety, or browser usability.

Primary references: [contextual tuples](https://openfga.dev/docs/interacting/contextual-tuples), [source of truth](https://openfga.dev/docs/best-practices/source-of-truth), [consistency](https://openfga.dev/docs/interacting/consistency), [immutable models](https://openfga.dev/docs/getting-started/immutable-models), [pinned ZITADEL linking options](https://github.com/zitadel/zitadel/blob/v4.19.2/proto/zitadel/idp.proto).

## Migration and first-use runbook

1. Schedule a maintenance window. Stop application traffic and background writers. Take a database backup and prove restoration into a disposable database. Do not inspect production credentials or infer production row contents from this repository.
2. Require explicit operator inputs for the legacy target edition label, festival start/end dates, and bootstrap organizer issuer/subject. Do not infer editions from note creation years or automatically grant every club-member organizer access. If there are no legacy rows, do not require a backfill target unnecessarily.
3. Use a versioned transaction to create/reuse the exact validated edition, add nullable feedback edition references, backfill all existing feedback notes to it, verify unchanged IDs/titles/CreatedAt and row counts, then enforce foreign-key/non-null constraints. An incompatible pre-existing target or any failed invariant rolls back the transaction. Preserve worker directory and lasting stands globally; scope participation, shifts, requests via their parent shift, assignments via their shift, and feedback notes to an edition. Never copy old requests or assignments to a new edition.
4. Missing required inputs fail before writes. Migration remains independent of OpenFGA availability, as the existing migration entrypoint is. Do not run AutoMigrate as a substitute for validated backfill. Rehearse reruns and rollback on a copy, including already-migrated rows; never reassign correctly scoped data on retries.
5. Prepare and pin the authorization model and identity-provider settings. Bootstrap one explicitly chosen login operationally; the last global organizer cannot be removed in-app. Exercise positive and negative permissions before reopening. Authentication alone grants no internal access.
6. GET / becomes role-appropriate scheduling navigation. Feedback listing and POST /feedback-notes require global organizer permission and the actual selected edition. GET /design-system is public and must render only static examples without private navigation/data. /public/, /static/, auth endpoints, healthz and readyz retain their public operational purpose; sign-in/callback/logout must not reveal or modify edition data. Replace club-member admission with authentication plus resource checks throughout private handlers. Preserve CSRF protection on mutating routes and bind callback continuation to the initiating browser.
7. Keep traffic closed after migration or authorization-validation failure. Before reopening, restore the tested backup if rollback is necessary. After new edition-scoped writes occur, use a forward fix; blindly restoring the old binary could expose all-edition notes or fail writes. Never reopen the old club-member gate to volunteers as an outage workaround.
8. Rehearse 10 stands, 250 workers, roughly 150 shifts plus old history. `rehearsal-fixture.json` supplies the synthetic ten-stand/250-worker/150-shift baseline for later integration rehearsal; it is not imported into any service. Pilot one prepared stand with a few workers: guest submission, optional account connection, approval, printing, and next-request revocation. Organizers deliver printed schedules/updates; no automatic assignment emails or SMS. Broader invitations wait for prototype approval and pilot sign-off. A serious pilot issue pauses application access under maintenance and uses the saved roster for coordination until repaired.

## Required application acceptance checks

- Two approvals/direct assignments for the final place: one success; no duplicate assignment; failed action leaves membership unchanged. Capacity reductions cannot undercut confirmed assignments.
- Multi-shift submission with a started/full/cancelled/ineligible shift writes no worker or requests; valid form selections remain visible. Retry is idempotent.
- Activation/revocation racing with mutations cannot authorize against stale Current Edition, role, participation, or worker binding. Failed/competing activation leaves one unambiguous committed pointer, reread after an unknown network outcome.
- Edition rename preserves graph identity and attached records. Date correction flags retained out-of-window shifts without moving them; new/time-edited shifts must fit. Corrected dates can revive a previously date-expired link, never a disabled/replaced one.
- Disabling participation retains history/team/requests/memberships, grants only global schedule reading, and leaves old own requests read-only. Current Edition retains at least one enabled stand and teams for all enabled stands.
- Bearer organizer invites are single-use, seven-day, revocable/replaceable, scoped exactly to the invited grant; opening does not consume them. Losing issuer global access invalidates unaccepted invites; concurrent redemption cannot grant twice. No recipient-email matching is required.
- Guests see no names/rosters/contacts/notes; members see only roster names/assignments; directory masking and worker-private notes honor existing worker resolution. Revocation and DB-operated worker unlink take effect next request.
- Migration preserves legacy note values and edition isolation. Test missing inputs, reruns, rollback, and auth outage before reopening.
- Run the phone guest flow and all organizer variants with real human feedback before closing the prototype and acceptance decisions.

## Validation performed

29 permission assertions passed against a checksum-verified OpenFGA v1.21.0 in-memory engine. Headless Chromium exercised all three layouts, guest atomic-validation/input preservation, optional mock account connection, old own-request read-only behavior, explicit global self-grants, activation, disabled participation, mobile horizontal overflow and authorization-outage rendering. No JavaScript page errors were observed. Screenshots were inspected. These checks do not replace human workflow review or a live pilot.

Prototype approval: https://github.com/tjrasche/HerbsfestOS/issues/8#issuecomment-6501083164

## Planning complete

The user approved the MVP definition. The acceptance decision and canonical Wayfinder map are closed because the criteria and first-use plan are agreed. Actual application implementation and the live pilot remain future release work; closing this planning map does not claim either has occurred. Acceptance resolution: https://github.com/tjrasche/HerbsfestOS/issues/9#issuecomment-6501128472
