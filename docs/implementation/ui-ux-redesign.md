# UI/UX redesign implementation plan

Status: implemented and verified, 2026-11-01. The new orchestrator and two parallel implementation workers completed the redesign in the existing Go/net/http, templ, htmx, PostgreSQL/GORM application. See [verification and run notes](ui-ux-redesign-validation.md) for actual test execution, browser/role/outage results, measurements and limits. The detailed plan below is retained as the implementation contract. This document authorizes no commit, push, deployment, production mutation, or infrastructure change.

## 1. Outcome and constraints

Make the German volunteer scheduling application usable on a phone and efficient for organisers handling ten stands and 150 shifts. Preserve the existing Ringinger Herbstfest identity, sidebar/header pattern, typography, backend domain rules, and ordinary links/forms. Use htmx for navigation and server-rendered interaction; use minimal vanilla JavaScript only for browser behaviour such as drawer dismissal, focus, request announcements, clipboard, and printing.

All records, permissions, validation, readiness, workflow decisions, and mutations remain on the server. URLs and submitted fields describe the requested view; they never grant access. Do not add a frontend framework, client domain store, browser persistence of records, speculative assignment updates, or client-side filtering of a privately downloaded directory. No Kubernetes, Flux, rollout, preparation, authorization-model provisioning, default settings, or deployment workflow changes are needed.

These existing rules are release gates:

- Derive permissions from the authenticated issuer/subject and the actual resource within a fresh scheduling transaction. Preserve the scheduling-state revision lock, lock ordering, pinned OpenFGA model, and fail-closed error distinctions.
- Global visibility never implies schedule editing. Editing/approval/direct assignment requires the explicit stand-participation team grant. Offer a deliberate self-grant only where the current policy permits it; never add one as a side effect of opening a page.
- Stand membership persists across editions. Participation, enabled status, and organiser teams belong to an edition. Changing the global Current Edition and selecting an edition personally are separate actions.
- Public signup requires only names; every valid volunteer submission remains a pending request. Preserve hard capacity, atomic multi-shift submissions, idempotent retry semantics, and atomic assignment plus membership.
- Directory results retain masking. A full profile permission does not grant visibility into every other stand's schedule or private relationships. Shared notes remain visible to the worker; workers can edit their contacts but cannot edit shared notes.
- Archived own requests remain available and read-only. Revoked grants, removed account bindings, revoked links, expiry, and issuer invalidation take effect on the next request. Keep single-use proof ownership and scope rules.
- Preserve `http.NewCrossOriginProtection`, body limits, validated OIDC continuation/state/nonce/PKCE, and no-store/no-referrer treatment. Never log bearer URLs, raw invitation tokens, personal form contents, or authorization credentials.

## 2. Evidence and implementation seams

Inspected: `cmd/web/routes.go`, `cmd/web/main.go`, `internal/scheduling/{handler.go,read.go,views.templ,store.go,catalog.go,workers.go,capabilities.go,model.go}`, the versioned SQL, UI layout/components/CSS/JavaScript, auth handlers, route/integration tests, browser script, README, GLOSSARY, and existing acceptance/authorization documents. No additional repository `AGENTS.md` was found in this checkout or its ancestors; follow the user-supplied host instructions. Preserve unrelated working-tree changes and signing configuration.

The review artifacts are in `/tmp/herbstfest-usability-review`. Existing findings include authenticated 390px layouts expanding to 653px, a roughly 64,000px schedule with 1,791 forms, a long mixed-scope admin page, hidden creation, plain-text errors, directory limits without disclosure, repeated assignment selectors, incomplete profile context, unsafe merge defaults, and thin guest/print/proof feedback.

Today `Handler.page` calls `Store.View` for most private screens. `View` loads editions, workers, accounts, schedules, every roster and pending list, members, teams, and feedback, with per-shift queries/checks. `views.templ` contains almost every scheduling screen and action. The old `internal/feedbacknote` UI remains in the tree but live `/feedback-notes` routes are registered by scheduling. Do not accidentally re-enable the unscoped legacy feedback service.

Keep `Store.Execute`/`Submit` as authoritative mutation boundaries. Add narrow read services and resource-specific HTTP adapters around them. Keep existing exported APIs temporarily for integration tests; migrate live handlers away from `View`. Do not rewrite proven assignment/link/merge transactions for cosmetic reasons.

## 3. Information architecture and routes

Use a small operational navigation: **Dienstplanung**, **Meine Anfragen**, **Helfer suchen** when permitted, **Organisation** for global organisers, and **Feedback** for global organisers. Include a reachable **Meine Angaben** destination for a linked worker. Remove Designsystem from operational navigation; retain `/design-system` as a directly reachable public development/reference page with no private navigation or data. Active links and breadcrumbs derive from a typed page/navigation model, never substring guesses.

The following paths are the implementation contract. `{participation}` always means the actual edition-specific participation ID, not the persistent stand ID. `{capability}` is a non-bearer record identifier derived from the existing digest; it is never accepted as a signup/proof token. Use query parameters only from the documented allowlists.

| GET destination | Purpose and context |
| --- | --- |
| `/` | Compact Dienstplanung, canonical `edition`, `stand` (participation ID), `day` (Berlin date), optional `page`. Default to current edition, one authorised stand and its first day with shifts. Query only that chosen stand/day; an explicit all-stands view remains bounded. Never render all shift action forms. |
| `/shifts/{shift}` | Focused shift detail in the same filtered schedule context; desktop side panel/main split and phone full-width detail. Back link returns to the validated schedule query. |
| `/participations/{participation}/shifts/new` | Visible creation screen, prefilled from selected day; explicit edit permission required. |
| `/shifts/{shift}/edit` | One shift form with version and contextual warnings. |
| `/requests/mine` | Own current requests first, archived groups after; optional `status`, `page`. Independent of selected edition restrictions on browsing other people's schedules. |
| `/requests/pending` | Accessible paginated queue for permitted schedule editors, filtered by `edition`, `stand`, `day`; global viewers without a team get counts/read-only context only where existing policy allows, no approval buttons. |
| `/directory` | Server search with `q`, `page`, optional validated assignment/membership context. |
| `/workers/{worker}` | Authorised profile, memberships/context, account status, contacts, shared notes, and scoped actions. |
| `/workers/{worker}/assign` | Single focused assignment chooser: permitted edition/stand/day first, then bounded shift results. Never repeat 150 options per search result. |
| `/stands/{stand}/members` | Persistent membership management, separately labelled from the edition team; optional `q`, `page`. |
| `/admin` | Organisation overview and guided first setup/readiness. |
| `/admin/editions`, `/admin/editions/new` | Edition list and discoverable creation. |
| `/admin/editions/{edition}` | Dates, enabled participation list, readiness, explicit Current Edition activation. |
| `/admin/editions/{edition}/participations/{participation}` | Participation status, edition-specific team, self-grant, scoped invitations, and link to that schedule. Validate parent-child relationship. |
| `/admin/stands`, `/admin/stands/new`, `/admin/stands/{stand}` | Persistent stand catalog and name editing, links to memberships and edition participation. |
| `/admin/access` | Global grants and global invitations; no mixing with persistent memberships. |
| `/admin/invitations` | Organiser invitation list, filter by scope/status, bounded pages. Team invite actions also appear on the participation page. |
| `/links/{capability}` | Authorised metadata/status and reissue/revoke controls for an existing signup/connection/invitation record; never recover raw tokens. Also reachable from scoped link sections. |
| `/admin/workers/merge` | Choose distinct target/source using server search; query `target`, `source`, then server-rendered comparison/preview. Link here from a worker profile with target prefilled. |
| `/feedback-notes` | Edition-scoped create/list, optional `edition`, `page`. |
| `/print/{participation}` | Dedicated compact confirmed roster, optional validated `day`; no operational chrome or forms. |
| `/register/{token}` | Public/optionally authenticated names-only request form using a minimal branded guest shell. |
| `/proof/{token}` | Minimal branded proof context, optional authenticated identity, followed by explicit acceptance. Opening never consumes it. |

Maintain public `/public/`, `/static/`, `/auth/*`, `/healthz`, `/readyz` boundaries. Keep `/admin` useful as an overview, not a redirect into another long page. Preserve old `/print/{id}` and public bearer paths. Redirect legacy `/#mine` navigation to the new own-request link in templates (fragments are not visible to the server). Stop generating `/actions` forms; retain its adapter only if needed for compatibility and subject it to the same responses and permission checks.

Resource-specific POSTs use ordinary HTML forms and map to existing commands:

| POST family | Existing command / expected result |
| --- | --- |
| `/admin/editions`, `/admin/editions/{edition}` | `edition-save`; new edition detail or preserved edit context |
| `/admin/editions/{edition}/activate` | `activate`; readiness must be checked again under the write lock |
| `/admin/stands`, `/admin/stands/{stand}` | `stand-save` |
| `/admin/editions/{edition}/participations`, `.../{participation}` | `participation`; parent and actual resource must match |
| `.../{participation}/team`, `.../{participation}/team/{account}/remove` | `team-add` / `team-remove` |
| `/admin/access/{account}/remove` | `global-remove`; preserve last-global guard |
| `/participations/{participation}/shifts`, `/shifts/{shift}` | `shift-save`, with creation vs edit resolved from path |
| `/shifts/{shift}/cancel`, `/shifts/{shift}/assignments`, `/shifts/{shift}/assignments/{worker}/remove` | `shift-cancel`, `assign`, `unassign` |
| `/requests/{request}/approve`, `/decline`, `/withdraw` | `approve`, `decline`, `withdraw` against the actual request |
| `/stands/{stand}/members`, `/stands/{stand}/members/{worker}/remove` | `member-add`, `member-remove` |
| `/workers/{worker}` | `worker-save`; notes handled with actual edit permission |
| `/admin/workers/merge` | `worker-merge` after preview, explicit confirmation and fresh comparison |
| `/participations/{participation}/links`, `/shifts/{shift}/links`, `/workers/{worker}/connection-links` | `signup-link` / `connection-link`; return one-time created-link surface |
| `/admin/invitations`, `/links/{capability}/reissue`, `/links/{capability}/revoke` | `organizer-invite` or existing link-kind issue/revoke rules |
| `/edition-selection`, `/requests`, `/feedback-notes`, `/register/{token}`, `/proof/{token}` | Preserve existing semantic boundaries with contextual response handling |

Never trust hidden `action`, account, stand, participation, or shift IDs to override the route resource. Adapters load/validate the actual relationship and the transaction rechecks permission. Do not make GETs activate editions, grant teams, revoke, consume proofs, create links, or assign workers.

## 4. Primary screens and interactions

### Schedule and request work

The schedule header shows edition, whether it is Current Edition, stand/day filters, a pending-queue count/link, and a primary **Dienst anlegen** action only for an editable selected stand. Default to one permitted stand/day in the service query, not a full snapshot hidden by templates; offer a bounded explicit all-stands choice where useful. When all stands are selected, the creation action asks for an editable stand instead of choosing silently. Global users viewing without team permission see **Nur Ansicht** and a deliberate **Mich zum Standteam hinzufügen** action in the relevant participation context.

Render an actual compact time grid: a labelled hour axis with chronological time proportional to position, stand columns/rows, and shift blocks showing title, local start/end, confirmed/capacity and status. Use server-computed positions/overlap lanes; cap the displayed day to occupied hours with sensible padding. Midnight-spanning shifts appear as labelled clipped segments on both days. Use a horizontally scrollable, labelled local timeline region when needed; the page itself must fit the viewport. On phones, select one stand by default and show a narrower time axis with blocks. This is one responsive timeline, with a focused detail surface and an accessible pending queue. Do not build three mandatory alternative layout modes.

Each block is an ordinary link with an accessible name including stand, date, time, title, staffing and cancellation status. Provide a compact chronological semantic list alongside/within the same component for screen readers and as the narrow-screen representation when the grid cannot be legible. It reads the same filtered summaries and has no hidden action forms. Do not use a visually drawn grid as the sole means of accessing shifts.

Focused detail loads only that shift's roster, permitted pending requests, warnings, and actions. On desktop preserve the timeline beside it; on phone show detail first with an explicit **Zurück zum Plan** link. Put edit, assignment and cancellation on focused forms. Never fetch contact details just to draw a shift block. Distinguish full/cancelled/past/read-only; capacity fullness means no new requests or assignments according to the existing service, not automatic rejection of already pending requests. Overlap markers reveal no other-stand private details.

The pending queue is reachable above the timeline and in focused detail. It shows person, stand, shift/date/time, stale-time warning, remaining confirmed capacity, and approve/decline. Use a page of 20 requests, with 40 individual decision forms at most; cap all list endpoints. Preserve queue filters/page after a decision, update counts from the committed result, and select a sensible remaining page when the last row disappears. A failed approval keeps the row and explains full/stale/changed context. Include the next row/action as the focus destination after success. No bulk approval is required.

Own requests are a dedicated top-level destination, also summarised near the schedule heading rather than after every stand. Show confirmed/pending/declined/withdrawn/cancelled labels, edition/stand/time, time-change warnings and a withdraw action only when the server says it is allowed. Archive groups contain no edit controls. A worker with no memberships receives a useful own-request/optional account context; a global user with no edition receives setup guidance.

### Organisation and first setup

`/admin` is a compact overview with Current Edition, selected edition, counts, and a clear next task. When no edition exists, show **Erste Ausgabe erstellen**, then **Stände anlegen/teilnehmen lassen**, **Standteams zuordnen**, and **Ausgabe aktivieren** as server-derived steps with direct links. A global user must never receive the generic contact-organisation dead end.

Edition creation is visible outside collapsed details. Explain that enabled participations/teams may be copied from the selected source while shifts start empty and persistent memberships remain. An empty source still follows existing command semantics (copy Current Edition if present); do not invent a new “copy nothing” choice without implementing it explicitly.

Readiness must reuse the exact activation predicates: valid dates, at least one enabled participation, and a team on each enabled participation. Show missing teams by stand and the action that fixes each one. Missing shifts, signup links or staffing are useful planning hints, **not** new activation blockers. The activation screen names the edition, explains the global effect, requires explicit confirmation, and revalidates readiness on POST. Date corrections preserve existing out-of-window shifts and mark them.

Stand catalog pages label names/memberships as persistent. Participation pages label teams and enabled status with the edition. Team removal/participation disable explains current policy consequences and the ready-state guard; if the command is blocked, keep the page and show how to resolve it. Global access pages identify the actual grant/account and retain the last-global restriction. Account selectors use linked worker names when available plus a short stable account reference; do not invent stored IdP display names, infer identity from email, or fetch broad provider data.

### Directory, profile, assignment and merge

Search has distinct states: initial explanation/minimum two characters, searching, results, no results, too-short/invalid query, unavailable, and additional pages. Preserve literal-name and exact-contact matching semantics. Return 25 rows per page with deterministic `(last_name, first_name, id)` ordering and a `HasNext` flag from a `limit+1` query. Show “weitere Treffer”/page navigation instead of an invisible 50-row cutoff. Avoid an exact total query if unnecessary; do not imply the shown count is the whole directory.

Each row shows only permitted distinguishing information: name, masked or full contacts, and a scope-safe membership/account label only if authorised. Masked results must contain no unmasked data in attributes, JSON, hidden controls, accessible labels, or links to a full detail page. Use a stable short reference where two names/contacts remain indistinguishable. One **Einteilen** or **Zum Stand hinzufügen** link opens a focused chooser with that person preselected and preserved search context. Selecting an assignment must keep the existing single atomic assign-plus-membership command; do not introduce a separate membership write as a prerequisite.

A profile header shows permitted contact-edit/read-only state, **Konto verbunden / Noch kein Konto verbunden**, authorised membership context, and relevant schedule/own-request links. Shared notes carry “Für diese Person und berechtigte Standteams sichtbar.” For a worker viewing their own notes, render readable text with an explanation of who can change it; avoid an unexplained disabled textarea. Do not accidentally blank notes when an own-contact form omits that field: load and preserve notes server-side, with the final transaction still enforcing `edit_notes` on any change. No account unlinking feature is introduced.

Merge starts from a selected worker, then searches for a different worker; never preselect the same person on both sides. Load a server preview containing names, contacts, account-link status, permitted membership/request/assignment counts, and consequences. Show target/source side by side on desktop and stacked with explicit labels on phone. Preserve the sole linked login, block conflicting distinct logins, preserve existing membership/assignment/request coalescing and audit snapshots, and explain invalidation of ownership proofs.

Prefill result values from the retained worker; if a contact is empty, offer the source value. For differing nonempty contacts require an explicit keep/source/custom choice; for notes offer a prefilled combination retaining both texts or an explicit edited result. Never silently overwrite existing contacts/notes with blank defaults. Explicit clearing needs an explicit clear control. POST carries a server-generated comparison fingerprint, rechecks it under the existing write lock, and returns a 409 refreshed comparison if either record/association changed. Preserve the proposed result values and require reconfirmation. Do not permit a crafted POST to bypass preview freshness or the explicit-clear rule.

### Links, invitations and proof

List existing authorised capabilities with type, edition/stand/shift or worker scope, creation time, expiry (or the existing no-expiry signup rule), issuer/consumed account context where permitted, and derived status: active, consumed, revoked, expired, or currently unusable. A lost issuer grant, disabled/archived participation, or current-edition gate can make a capability unusable without making it “revoked”; label accurately. Listing/status does not consume or reactivate anything.

Capabilities currently store digests, not recoverable raw tokens. Keep that property. Add lookup/revoke/reissue by a non-bearer record reference with the same kind-specific permissions as current token-based commands; this removes pasted-token revocation without storing new plaintext secrets. Existing single-use/replacement semantics remain: connection/signup replacement invalidates the relevant old link; organiser reissue identifies and invalidates the specific old invitation and retains scope/participation. Do not offer reissue of a consumed proof as if it were unused; route to the appropriate new invitation/connection action according to current state.

After issue/reissue show the absolute same-origin URL in a readonly selectable field, expiry/scope, **Link kopieren**, and **Zurück zu …**. Build the origin from the configured trusted application base URL (development may use an explicitly configured loopback origin), never arbitrary `Host`/forwarded headers. Clipboard failure reveals/selects the field with a manual-copy instruction. Raw tokens appear only in the immediate created-link/guest-proof response, remain no-store/no-history/no-referrer, and are never added to list records or logs. Issue/reissue POST returns a 200 one-time result surface; do not push a POST/token result into navigation history or automatically retry it. The normal browser's resubmission warning remains meaningful; a lost result requires an explicit replacement action.

Proof GET must use optional authentication so a signed-in person sees their current account context. Replace `InspectProof`'s kind-only output with minimal, bearer-authorised proof context: purpose, organiser role and edition/stand scope or worker name for a connection, expiry, and whether this identity can proceed. Do not expose contacts, notes, rosters, or unrelated grants. Logged-out users see only the sign-in/register continuation action; no acceptance POST button that immediately fails auth. After login return to the proof context, show the account accepting it, and require explicit acceptance. Consumption and issuer/scope/expiry/account conflicts are rechecked in `redeem`, never decided from displayed metadata. Already-used/expired/revoked links get specific safe recovery text without disclosing someone else's consuming identity.

### Guest signup, feedback, printing and errors

Guest pages retain the brand and lightweight header/footer but omit workspace/sidebar/admin/session chrome. Names stay the only required personal fields. Group selectable shifts by Berlin day, show capacity/status and clear selection instructions, preserve names, submission key and selections on validation/conflict. Explain per-field name errors and per-shift ineligibility from server results. When any batch member fails, state that **nothing from that batch was saved**. Do not label a pending request as a confirmed place.

Extend `Submission` with an own-submission receipt built from canonical server shift/participation/edition data: submitted names, selected shift titles/dates/times/stand and request status. Reconstruct the same scoped receipt in the fingerprint-matched idempotent retry branch; do not use an arbitrary worker ID as a public receipt credential or include rosters. A successful batch lists the actual accepted selections before offering optional account connection. Existing stored submission keys/fingerprints remain authoritative; a receipt does not create a general public worker endpoint. Retain correct behaviour if an old submission is retried after signup expiry.

Feedback remains selected-edition scoped and global-only. The live scheduling backend accepts a 200-character title; replace the reused 120-character field with matching frontend validation/help and server errors. Preserve entered text on failure and show immediate contextual success/list update.

Print only confirmed names for noncancelled shifts. Group by day with compact tabular rows: time, shift, confirmed names/capacity where useful. Repeat festival, edition, stand, day, and column identity on each printed page using a table header that Chromium repeats; do not rely only on a first-page heading or browser margin headers. Use readable print sizes, avoid clipping and large empty cards, and allow very large rosters to break safely while repeating identity. Show a print button and back-to-filtered-plan link on screen only. Include generation timestamp and short handling context; no notes, contacts, pending requests, hidden forms, or private bearer URLs.

403, genuinely missing 404, conflict 409, validation 422 and outage 503 use branded recovery screens or contextual panels with correct HTTP status. Distinguish “not found” internally from malformed input; inaccessible resource IDs must not become an enumeration channel. Provide safe back-to-schedule/own-requests/start links and an explicit retry for transient outages. If fresh authorization is unavailable, render a minimal authenticated shell without protected records, old counts, or privileged navigation. Never use a remembered role to keep private content visible. Do not retain stale sensitive content after a revocation response.

## 5. Read models, queries and service contracts

Put request parsing/canonicalization in HTTP code, permissions and disclosure decisions in Store/Tx reads, presentation-only German formatting in view helpers. Use typed inputs/outputs, not a growing map or one omniscient `View`. The exact exported names may be adjusted once by the orchestrator before workers begin; preserve these boundaries:

| Service contract | Minimum result and query/disclosure boundary |
| --- | --- |
| `Shell(ctx, identity, edition)` | Navigation capabilities, own worker/account reference, Current/selected edition labels and authorised edition choices. No schedules, directory, rosters or feedback. Check engine availability even for a signed-in identity without grants. |
| `ScheduleSummary(ctx, identity, ScheduleFilter)` | Authorised stand/day choices and compact shift summaries: IDs, title, interval, capacity/confirmed/pending counts when permitted, cancelled/outside/overlap markers, permitted links. Resolve selected edition and participation server-side; no contacts/notes or per-shift forms/requests. |
| `ShiftDetail(ctx, identity, shiftID)` | Actual participation/edition, roster names, bounded pending page, free places, version, explicit action booleans. No all-worker/all-shift selects. |
| `OwnRequests(ctx, identity, filter)` | Own current/archive rows, canonical stand/time/edition labels, status, stale flag and fresh `CanWithdraw`. |
| `PendingRequests(ctx, identity, filter)` | Page of permitted request rows plus safe counts/next page; verify permission for each participating actual resource. |
| `OrganisationOverview` / `EditionDetail` / `StandDetail` / `AccessDetail` | Only the relevant catalog/team/grant data, exact readiness failures and next-step links. No schedule rosters or whole worker directory. |
| `SearchWorkers(ctx, identity, DirectoryQuery)` | Page of already-masked display rows and `HasNext`, query state, safe context. Use exact-contact/literal-name rules; do not disclose memberships/account IDs merely because a name matches. |
| `WorkerDetail(ctx, identity, workerID)` | Contacts/notes by existing worker permission, separate `CanEditContacts`, `CanEditNotes`, `CanIssueConnection`, linked/not-linked status, and individually authorised context. No automatic disclosure of other stands' schedules. |
| `AssignmentChoices(ctx, identity, workerID, filter)` | Authorised editable participations and a bounded set of canonical shift choices; worker identity is explicit, selection is a request input. |
| `Capabilities(ctx, identity, scope, cursor)` / `CapabilityDetail` | Non-bearer reference, safe labels/status and action booleans; kind-specific management permission, no raw token or unrestricted digest table dump. |
| `ProofContext(ctx, token, optionalIdentity)` | Minimum context justified by a valid proof; no consumption; availability and eligibility checks consistent with redemption. |
| `MergePreview(ctx, identity, target, source)` | Global-authorised two-record comparison, result defaults, conflict reason and fingerprint over relevant record/account/association state. |
| `Signup` / `Submit` receipt / `PrintSchedule` / `FeedbackList` | Keep public, own-submission, confirmed-print and edition-feedback data separate from private management models. |

Within each service acquire the existing revision lock before reading trusted facts. Prefer one transaction to build a page model: expose internal `Tx` helpers so shell and page data can share the same snapshot instead of nesting transactions or calling expensive `View`. If shell and body must be separately read initially, mutations still re-authorise and no fragment is emitted until both succeed; converge to the shared transaction before final acceptance.

Batch fetch counts/rosters for the **filtered page**, using grouped joins/CTEs instead of per-shift SQL. Preserve empty shifts with left joins; avoid count multiplication by independently aggregate assignments and pending requests. Derive permission per actual participation/worker; batching data never means treating a global role as an edit grant. Reuse request-local checked permissions for the same resource/snapshot; never cache across requests. Do not run mutable `Tx.facts` checks concurrently on one transaction.

A day includes shifts overlapping its Berlin midnight interval (`starts_at < nextDay AND ends_at > dayStart`), not just shifts that start that day. Server computes offsets/lanes without altering stored shift times. Invalid filter IDs/dates return inline filter feedback or a clear 404/403 as appropriate; they must not widen the query to all data. Bound page sizes and query lengths server-side regardless of submitted controls.

New UI services should work against the existing schema. For capability management use the stored digest as an opaque non-bearer key and add a distinct command field/path adapter; never reuse the raw-token acceptance path. Merge fingerprint and receipt can be generated/reconstructed from existing records without storing new client data. Add an application migration only if measured query plans prove a necessary index, and keep infrastructure/configuration unchanged. This plan does not require a schema or authorization-model change.

## 6. HTTP, htmx and context contract

### Navigation and response shape

Use one predictable frame contract to avoid ad hoc fragments. A full document contains scripts/styles, `#app-frame` (sidebar/header/main/footer or guest shell), and persistent `#page-loading` and `#app-status` regions. A navigational htmx GET returns `<title>…</title>` and exactly one replacement `#app-frame`, with no second document/head/scripts. All page links keep real `href`; the navigation-link component adds htmx boosting/get, `hx-target="#app-frame"`, `hx-swap="outerHTML"`, and `hx-push-url="true"`. Do not enable blanket boosting that accidentally changes every form or external/auth/print link.

Give main content `id="main"` and `tabindex="-1"`; include real breadcrumb links and `aria-current="page"`. Same-route filter/search forms are normal GET forms with explicit htmx GET/frame target/push URL. Detail opening is a real GET route and the server renders the same responsive layout on refresh/deep link. No JSON hydration or domain data script is required. Since filtered pages are bounded, frame replacement is acceptable; smaller partials are optional only after correctness, with an explicit typed target and identical privacy/error handling.

Resource mutations use explicit `hx-post` to the same `action`, target `#app-frame`, and swap `outerHTML`. Server responses re-render the focused context with inline form errors or contextual success and refreshed counts/permissions. Plain HTML POSTs return 303 to a canonical GET on success; 422/409 re-render a complete document with the submitted values and correct status. htmx success returns 200 with the frame and `HX-Replace-Url` when the canonical GET destination changes, rather than pushing the POST URL. A one-time created-link or guest receipt can return a 200 result surface to a plain POST as described above. Never put `HX-Location`/`HX-Redirect` on a 303 and expect htmx to consume it before the browser follows the redirect.

Use `Vary: HX-Request, HX-History-Restore-Request` and private `Cache-Control: no-store` on every sensitive full/fragment/error response. Preserve no-referrer on signup/proof/link/print pages (it is also safe to retain globally). Full/partial detection must validate the htmx header value and force a full document for history restoration. Do not trust `HX-Target`, `HX-Current-URL` or `HX-Request` for authorization or as an arbitrary redirect destination.

### Protected history and authentication

Set `hx-history="false"` on protected and bearer-containing bodies/frames, and set htmx config `historyRestoreAsHxRequest: false`. Set `historyCacheSize: 0` for this app so no private page snapshots are intentionally retained in htmx local storage. Maintain URL navigation with pushState, but a back/forward history miss must make a fresh server request and return a **full document**, including when `HX-History-Restore-Request: true` is present. Use `refreshOnHistoryMiss: true` if needed to make the full-document path deterministic; verify actual htmx 2.0.11 behaviour in Chromium rather than assuming config prevents all restoration paths. No private/token page may be restored from an htmx snapshot after logout/revocation.

Test browser back/forward and bfcache as well as htmx history. On browser `pageshow` with `persisted` in a protected/bearer document, a minimal browser-lifecycle handler may reload for fresh authorization; it must not reconstruct records or permissions in JavaScript. Clear any old htmx history cache left by the previous UI using the library's supported cache event/key behaviour, without storing new private content. Do not erase unrelated site storage.

Unauthenticated htmx private requests use a full `HX-Redirect` to `/auth/login?next=…`, with a validated local canonical continuation for the requested GET. Do not swap the IdP/login document into the frame. Normal auth links are full navigations. Public `/register` remains anonymous under development auth; proof uses `Optional`, not `Protect`, on GET and `Protect` on acceptance POST. OIDC continuation binding/expiry remains intact. Auth middleware must never replay an interrupted POST automatically after login.

### Safe return destinations and edition context

Use a typed `ReturnContext` carrying known GET route, resource IDs and allowed filters. A hidden `return_to` is an untrusted suggestion, not a stored privilege. Parse it with `net/url`; require a local absolute path, no scheme/host/userinfo/`//`/backslash/control characters, bounded length, and no nested auth/bearer/POST route. Allowlist recognized route patterns/query names, validate dates/page/edition IDs, and verify the referenced resource/edition is visible now. Discard unknown fields. On failure use a destination derived from the actual mutated resource, then a safe role-appropriate landing page. Never reflect a raw Referer or htmx current-URL header into redirect headers.

Canonical URLs explicitly carry a global user's selected edition and stand/day context; navigation values are only request inputs. Keep business state and decisions server-side. Do not introduce a browser filter/edition store. The existing edition cookie may be read only as a validated legacy preference during transition, then stop writing it when all links/forms carry canonical context. A stale/forbidden legacy selection must be ignored/cleared rather than blocking all pages or granting archive access.

`POST /edition-selection` validates that this actor may select the destination edition, then maps the current screen to the corresponding authorised edition context. Preserve directory `q`, feedback route, page type and same persistent stand when it has a participation in the new edition; reset day/participation only where invalid and explain the change. Changing a selection never activates the edition. Activation explicitly updates Current Edition and leaves personal selection clear in the resulting view. An archive selection by a global editor still obeys current-edition schedule-edit gates.

### Loading, validation and feedback lifecycle

Use reusable server-rendered loading markup: a top navigation progress indicator and skeleton blocks for the affected list/detail frame. `hx-indicator` reveals these during requests; use CSS for animation with reduced-motion support. Do not fake delay, download data twice, or leave skeletons in the completed page. Initial server HTML renders real data; skeletons support subsequent htmx navigation/search/detail loading. Show skeletons over an appropriately bounded region while keeping a clear loading label and preventing interaction with stale actions.

Forms include `hx-disabled-elt="find button[type='submit']"` (include relevant dangerous/duplicate actions as appropriate), a visible pending verb such as **Wird gespeichert …**, and `hx-sync="this:drop"` for non-idempotent writes. Keep field values present in the request; never disable inputs before serialization. Search uses a small delay/changed trigger and replace/abort synchronisation so a slow earlier query cannot overwrite a later one. Ordinary submit/search buttons remain available. No auto-retry of mutation POSTs on timeout.

Use small delegated JS event handlers only to toggle `aria-busy`, announce request/completion/failure, restore buttons after all outcomes, and direct focus. On full page navigation focus the new h1/main. On validation focus the error summary/first invalid input; on queue row removal focus the next logical action; preserve focus when a same-input search refreshes results. Stable unique IDs derive from resource/form identity, not repeated bare field names.

Configure htmx `responseHandling` deliberately: swap 422 and 409 frames for field/conflict feedback, and render 403/404/503 safe error frames when the handler supplies the frame contract. Maintain correct HTTP statuses. Handle unexpected response/network/timeout errors with a persistent local error notice and retry GET; do not inject arbitrary response text or blank the shell. If authorization is denied/unavailable, replace protected content with the safe error frame. This takes precedence over preserving old private content. A network-only failure can keep unsubmitted input with a clear “not saved” message. Distinguish a mutation whose outcome is unknown after disconnect: say it could have completed and offer a fresh GET before retrying.

`FormState` contains submitted values, field errors, summary, a safe form identifier, and contextual message. Validation is server authoritative; required/type/min/max/maxlength are progressive enhancement. Frontend and backend limits must match per field, including byte-vs-character constraints where existing contact/notes validation uses byte lengths. Do not expose a success message in an unvalidated URL parameter; derive it from the mutation response or a bounded allowlisted success code. Plain POST/redirect success can use an allowlisted code without echoing user-provided prose, or a server-side flash if an existing session mechanism supports it.

For time forms accept `datetime-local` in Europe/Berlin and derive the offset server-side for ordinary dates. Validate round-trip Berlin local time; reject spring-forward nonexistent times. Only if two instants share the autumn clock-change local time ask the user to choose the first/second occurrence with explicit offset. Preserve the existing RFC3339 command contract, edition time window, overnight intervals, confirmation of changed staffed times, and version check. Do not silently normalize an invalid local time or force an offset selector for normal festival dates.

## 7. Component contracts and responsive/accessibility rules

Reusable components each get their own `.templ` source, with associated Go types/helpers in `.go` as needed. Avoid another all-purpose `components.templ`/`views.templ` accumulation. A page may compose several components; do not create an abstraction for every line of static text. Generate `_templ.go` with the pinned tool; never hand-edit generated files.

| File/component boundary in `internal/ui` | Contract |
| --- | --- |
| `layout.templ` / `layout.go` | Full document and AppFrame with typed `PageMeta`/`ShellModel`, guest/private variants, history config, title and stable targets. Keep a compatibility wrapper while migrating design-system/legacy callers. |
| `navigation.templ` / `navigation.go` | Typed nav items, real href, active state, permission-filtered items and mobile navigation. |
| `breadcrumbs.templ` | Ordered links/current label; wraps safely on narrow screens. |
| `page_header.templ` | Existing brand eyebrow/title/description/actions, responsive action placement. |
| `button.templ` | Explicit primary/secondary/quiet/danger variants; real button vs real link; pending label/indicator; typed variant allowlist. |
| `form_field.templ` | Unique ID/name/label/value, hint/error IDs, `aria-invalid`, attributes; support textarea/select/checkbox without duplicate IDs. Never infer required from “all fields”. |
| `form_feedback.templ` | Error summary with field links, inline errors and contextual success/conflict; accessible live status. |
| `notice.templ`, `badge.templ`, `empty_state.templ`, `card.templ` | Small explicit tone/text/children contracts; no implicit role or permission logic. |
| `loading.templ` | Spinner/progress and skeleton variants for page/list/detail with text and hidden decorative shapes. |
| `pagination.templ`, `filter_bar.templ` | Canonical GET links/form context, current page, previous/next and reset, no client dataset. |
| `confirm_action.templ` | Server-rendered confirmation surface with named object/consequence, explicit confirm control and cancel link; no JS-only confirmation requirement. |
| `copy_link.templ` | Readonly absolute URL plus button/status/manual fallback; no hidden token storage or auto-copy. |
| `error_page.templ` | Status-specific safe branded recovery within full/fragment contract. |

Feature components live with scheduling: `schedule_timeline.templ`, `shift_summary.templ`, `shift_detail.templ`, `shift_form.templ`, `request_row.templ`, `request_queue.templ`, `own_requests.templ`, `edition_readiness.templ`, `edition_form.templ`, `stand_form.templ`, `team_list.templ`, `directory_results.templ`, `worker_form.templ`, `membership_list.templ`, `merge_preview.templ`, `capability_list.templ`, `proof_context.templ`, `signup_form.templ`, `submission_receipt.templ`, `print_roster.templ`, and appropriately scoped page files. A reusable feature component also gets its own source. Remove duplicate template function definitions as they move out of `views.templ`.

Responsive requirements:

- Desktop sidebar remains; below the chosen ~760px breakpoint use a **Menü** button and accessible drawer/disclosure with all permitted destinations reachable, not one unwrapped flex row. A no-JS disclosure remains usable. If enhanced as modal, provide Escape, outside dismissal, focus containment, focus return, and sensible `aria-expanded`/`aria-controls`; do not claim modal semantics without implementing them.
- Grid/flex children use `min-width: 0`; text/long identifiers/links wrap; forms use `width: 100%`/border-box; toolbar controls stack. The topbar can wrap and is not a fixed-height clipping box. A local labelled timeline scroller is the only intended horizontal scroll.
- Interactive targets are at least 44px where practical; visible keyboard focus; labels and fieldsets; no colour-only staffing/pending/error meanings; contrast matches the retained brand. Destructive remove/revoke/cancel/merge actions use danger/secondary hierarchy and explicit contextual confirmation where consequential.
- Use semantic headings, landmarks, real tables for tabular data, `time datetime`, and live regions sparingly. Do not make a whole card contain nested interactive links/buttons. Htmx updates must not repeatedly announce every skeleton cell.
- Test at 390×844 and 1440×1000, plus 320px/768px and zoom/reflow. The admin menu, forms, dialogs/panels, breadcrumbs, name/contact strings and success/error text must all remain reachable without document overflow.

Templ correctness is a specific acceptance item: HTML boolean attributes use `checked?={...}`, `disabled?={...}`, `selected?={...}`, `readonly?={...}`, `required?={...}`. `checked={false}` still creates a present HTML boolean attribute and is incorrect. ARIA booleans use their string/typed ARIA value as appropriate. Verify actual emitted attributes and interaction in Chromium, not just successful compilation.

## 8. Request and performance budgets

These are initial measurable budgets on the existing local 10-stand/250-worker/150-shift fixture, not production latency promises. Record baseline and resulting measurements with data size and environment. Correct authorization beats an artificially low query count.

- Schedule defaults to one day; bounded summaries across ten stands, at most 100 visible shift summaries per response, paginate more. A chosen stand/day usually has substantially fewer. A detail page contains one shift's forms only. No worker/account catalogs, 150-option selectors or hidden edit forms on the summary.
- Uncompressed schedule/frame HTML target ≤180 KiB; focused detail target ≤100 KiB with a bounded pending page. Directory target ≤80 KiB for 25 rows. Summary page form count target ≤8 excluding an explicitly opened detail surface; focused detail ≤30 forms with a ten-row embedded pending list. Pending queue 20 rows remains ≤45 forms. Adjust thresholds only with recorded evidence and bounded data, never return to per-shift action rendering.
- After initial static assets, one GET per navigation/filter/detail/search request; no per-cell fetches, no background polling loop, no N+1 HTTP calls. An ordinary mutation uses one POST and at most one follow-up canonical GET; an htmx mutation may return the fresh frame directly. Search cancels/replaces superseded requests.
- Batch SQL is proportional to the selected page/resources. Aim for ≤20 SQL reads for a schedule summary plus necessary per-participation permission fact/check work; detail/profile should not scale with the total 150 shifts. Record SQL and FGA counts separately. Do not convert the existing policy into unchecked SQL to meet a number. An explicit target of no per-shift roster/pending query on summary is mandatory.
- On warm local services target p95 ≤1 second for schedule/search/detail across 20 sequential reads; measure rather than assert. Htmx loading/pending feedback must be visible under a deliberately delayed 800ms response, and settle correctly on 422/409/403/503, timeout and abort. No runtime JS exceptions or htmx target errors.
- The 150-shift fixture must be navigable without a tens-of-thousands-pixel first screen. At 1440×1000 the default filtered summary should fit roughly within three viewport heights excluding an intentionally opened detail; phone flow must expose filters and a useful first result within the first viewport. Avoid a huge empty 24-hour axis; bound it to occupied intervals.

## 9. Parallel implementation ownership and phases

Create the **new orchestrator** after planning. It owns task decomposition, integration and verification and keeps the parent/user informed. Exactly two implementation workers can proceed in parallel once shared contracts are checked in the working tree; neither commits/pushes/deploys. Do not launch overlapping workers on the same shared template/function files.

### Phase 0 — orchestrator foundation and contract freeze

1. Read this plan and current user constraints; inspect status and the actual running fixture. Record the existing build/test baseline. Do not reset unrelated changes or reseed an already populated database.
2. Establish `PageMeta`, `ShellModel`, `FormState`, `ScheduleFilter`, `ReturnContext`, response helpers, and route registry boundaries in small Go files. Add the shared rendering/status/header contract and compatibility wrappers. Publish exact signatures and DOM IDs to both workers before they use them.
3. Extract route registration from the current `handler.go` into orchestrator-owned `routes.go` calling distinct worker registration helpers. Keep old routes temporarily. Reserve existing `handler.go`, shared response/context files, `read.go` compatibility code, `cmd/web/*`, and `internal/auth/*` to the orchestrator.
4. Assign the contracts below, especially `ProofContext`, receipt shape, capability record ID, and shared UI function names. Skeleton bodies can be temporary compilation scaffolds, but remove them before acceptance. Share compiler generation responsibility to avoid simultaneous broad formatting/generation on moving files.

### Worker A — shell, scheduling, public flow and print

Owns `internal/ui` reusable components/layout/navigation/CSS/app.js (excluding predeclared orchestrator shared Go seams), feature `schedule_*`, `shift_*`, `request_*`, `own_requests*`, `signup_*`, `submission_receipt*`, `proof_context.templ`, `print_*` templates, and new scheduling files `http_schedule.go`, `http_requests.go`, `http_public.go`, `http_print.go`, `read_schedule.go`, `read_requests.go`, `read_public.go`, `read_print.go`, `time_input.go`. Own the new browser suite portions for shell/schedule/public/print in a separate script/module; do not modify the shared top-level browser runner concurrently.

Deliver responsive shell, frame/loading/error components, real timeline and focused detail, bounded pending/own requests, shift forms and DST parsing, guest/proof presentation and receipt rendering, compact print. Add focused handler/query/time tests in uniquely named files. Use B's proof/receipt APIs and request modifications through the orchestrator rather than editing `capabilities.go`.

### Worker B — organisation, people, invitations and domain read additions

Owns organisation/directory/profile/membership/merge/capability/feedback page and component `.templ` sources, new scheduling files `http_organisation.go`, `http_workers.go`, `http_capabilities.go`, `http_feedback.go`, `read_organisation.go`, `read_workers.go`, `read_capabilities.go`, `read_feedback.go`, and the existing `workers.go`/`capabilities.go` for narrow merge/capability/receipt enhancements. Own uniquely named service/handler tests and a separate browser module for these flows.

Deliver guided setup/readiness, split edition/persistent stand/team/access management, directory pagination/focused assignment chooser, profile context, safe merge preview/freshness/clear semantics, capability list/detail/revoke/reissue, minimal proof context service, canonical `Submission` receipt data (including retries), and edition feedback. Keep existing `Submit`/redeem semantics. Request shared CSS/component additions from A with concrete markup requirements; put temporary feature styles in a separately owned `organisation.css` only if necessary and have A integrate/import it once. Never both edit `style.css`.

### Orchestrator — shared mutations, auth, integration and verification

Owns shared route/context/response files, `cmd/web/routes*`, auth continuation/middleware optional-proof integration, existing `catalog.go` and `store.go` if shared Tx helpers need changes, compatibility removal from `handler.go`/`read.go`/`views.templ`/`components.templ`, top-level browser runner, final README/validation notes, and generated-file consistency. Workers tell the orchestrator precisely which old template definitions to remove; the orchestrator does one controlled cleanup after their replacements exist. If generation is run by each worker, assign only their template paths and do not run broad `templ fmt .` concurrently.

The orchestrator owns the read permission review across all new endpoints, central error/return handling, and any newly needed mutation command fields. Worker B may edit its assigned domain files, but shared `Command`/`Result` additions in `catalog.go` are integrated by the orchestrator from an agreed patch/spec. No authorization-model edit is expected. Do not let a worker make a generic “global can edit” convenience helper.

### Phase 1 — independent vertical slices

A completes one real schedule/detail/edit/request and one public signup loop using the shared frame. B completes one real edition/setup/team loop and directory/profile loop. Each can use the running synthetic services; both keep their own test data isolated. Integrate early enough to verify navigation, context, component contracts and HTTP error shape before expanding to every screen.

### Phase 2 — cross-feature completion and cleanup

Integrate receipts/proof optional auth, focused directory assignment, membership management links, self-grant/readiness, link creation/status, merge guards, feedback and print. Migrate all generated forms off generic `/actions`, remove duplicate old template functions and stop live `Store.View` usage. Retain compatibility only when justified by actual tests/callers, not as a second divergent UI. Fix functional regressions before cosmetic polish.

### Phase 3 — integrated verification and review

Run generation/build/unit/integration/vet once the combined application compiles, then the browser matrix below. Exercise both ordinary requests and htmx responses, with and without JavaScript. Visually inspect desktop/phone screenshots and multipage PDF. Fix every material accessibility/privacy/permission/flow regression and rerun only affected checks plus final required suite. Summarise outcome, actual test execution/skip status, limits and changed files. Do not commit, publish or deploy without explicit user authorization.

## 10. Verification and acceptance matrix

Known local disposable services (confirmed by parent during planning): app `http://127.0.0.1:58080`; OpenFGA `http://127.0.0.1:58180`; browser fixture database `scheduling_rehearsal`; integration DB `postgres://tjark@127.0.0.1:55432/scheduling_test?sslmode=disable`. The synthetic app identity is `urn:herbstfest:local` / `developer`. Reuse these services if healthy; do not inspect credential files or touch live data. The test helper creates/drops isolated schemas and fresh FGA stores. Verify that tests actually execute rather than skip.

Tools are `/tmp/herbstfest-tools/go/bin`, `/tmp/herbstfest-tools/bin` (Kustomize), and `/tmp/wayfinder-validation/browser-env/bin/python` (Playwright Chromium). Read current README for any changed paths. No provisioning or deployment is part of this redesign.

Required integrated commands, with a task-specific PATH prefix rather than overriding HOME:

```sh
PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH go tool templ generate
PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH \
  SCHEDULING_TEST_DATABASE_URL='postgres://tjark@127.0.0.1:55432/scheduling_test?sslmode=disable' \
  SCHEDULING_TEST_FGA_URL='http://127.0.0.1:58180' go test ./...
PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH go vet ./...
PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH go build ./cmd/web
git diff --check
```

Update/replace `scripts/test-scheduling-browser.py` to assert new behaviour rather than its old 150-visible-articles expectation. Run it with `/tmp/wayfinder-validation/browser-env/bin/python`. Keep output/screenshots/PDF in a new temporary redesign validation directory, not over the independent review evidence. If the app process predates changes, restart only the disposable local web process with its existing authorized configuration; do not restart shared infrastructure or print its environment/credentials.

| Area | Required acceptance evidence |
| --- | --- |
| Setup | Fresh isolated global-organiser database opens guided setup. Create edition/stand/participation, show missing team, explicit self-grant, activate. No generic contact dead end. Activation guard matches service and cannot be bypassed by POST. |
| Permissions | Global without team sees schedule but cannot edit/approve/assign; explicit team enables permitted current actions. Stand organiser sees only allowed current resources; forged parent/edition/stand/query/return IDs fail. Volunteer cannot access directory/admin/pending management. Disabled/archive gates retained. |
| Revocation/outage | Remove team/global/binding/link then next GET/POST and htmx/history restore deny appropriately. Simulated/unreachable FGA gives 503 safe shell and no writes/old private content. Existing authorization race tests still pass. Never stop the shared FGA process just to create an outage test; inject a failing checker/test route in isolated test construction. |
| Shell/responsive | Chromium screenshots at 1440×1000 and 390×844 for schedule/detail/admin/directory/profile/merge/feedback/guest/proof/errors. At 320px/768px and zoom, document `scrollWidth <= innerWidth` except an intentional local timeline scroller. Every nav destination reachable; drawer keyboard/escape/focus, active nav/breadcrumbs and long-name wrapping work. |
| Schedule/performance | Actual fixture 10 stands/150 shifts, filters restrict server response, compact timeline has proportional intervals and cross-midnight grouping; no all-shift action forms. Measure HTML/forms/SQL/FGA/request counts and heights against budgets. Detail, queue and search use bounded pages. |
| Actions/feedback | Save/approve/decline/withdraw/create/disable/remove show pending disabled submit, visible loading/skeleton, contextual success and correct focus. Artificial delay exercises loading; duplicate click cannot cause unwanted duplicate issue/create. No htmx target or JS errors. |
| Validation/conflicts | Invalid dates, blank names, feedback length, bad capacity, stale version, insufficient capacity and concurrent approval return 422/409 with shell, values, filters, inline errors. Existing atomic capacity/batch/assignment-membership tests pass. Unknown-outcome network message does not claim definite failure. |
| Time | Normal Berlin dates require no timezone dropdown; overnight interval works; spring gap rejected; autumn ambiguity requires explicit choice; edition window/date corrections preserved. Actual emitted checked/disabled/selected attributes match true/false. |
| Directory/profile | Initial/short/no-result/many-result states; >50 matches reached via pages; deterministic ordering; wildcard remains literal; exact contacts retain semantics. Masked results contain no private attributes/notes/IDs granting access. One focused assignment chooser preserves search; profile notes cannot be changed by own-contact POST and omitted notes remain intact. |
| Merge | Distinct workers, prefilled result, explicit conflicts/clears, sole account preserved, two accounts blocked, membership/request/assignment/audit semantics retained. Stale preview POST returns 409 and proposed values; blank/tampered POST cannot silently erase existing data. |
| Links/proof | Absolute copyable link/manual fallback; safe origin; list statuses without raw-token recovery; contextual revoke/reissue by record reference. Logged-out proof has no failing accept button; login continuation returns to proof; signed-in account/role/scope visible; GET does not consume; concurrent/expired/revoked/conflicting redemption tests pass. |
| Guest | Phone names-only signup; accessible selected shifts, precise inline feedback, atomic failure preserves names/key/selections. Receipt lists actual accepted selections and pending state, no roster/admin chrome. Idempotent retry including after expiry reproduces own scoped receipt without duplicate requests. Optional connection remains optional. |
| Own/archive/feedback | Own requests reachable before schedule end; archived rows visible and read-only; current withdrawal rules fresh. Feedback remains isolated by edition and accepts the backend's matching 200-character limit with preserved invalid text and success. |
| Print | Chromium print PDF spans multiple pages as needed; each page repeats festival/edition/stand/day identity; readable compact chronological rows; no pending/cancelled/contact/notes/forms/sidebar. Inspect screenshots of page one and later page. |
| HTTP/history | Every canonical screen works as direct full GET, htmx frame GET, refresh/deep link, back/forward and no-JS navigation. 422/409/403/404/503 correct status/rendering, headers no-store/no-referrer/Vary, no nested document/script duplication. No protected HTML in htmx localStorage history; restoration/bfcache reload re-authorises. Login uses full redirect with safe continuation, never embeds IdP HTML. |
| Scope | `config/` and GitOps defaults unchanged; no unauthorized commit/push/deploy/live invitations; no credential inspection or exposure. Existing OIDC/model/migration tests retained. |

Prefer tests proving new semantic boundaries (permissions/disclosure, safe return parsing, no-state-loss validation, receipt retry, merge freshness, DST ambiguity and fragment/history shape). Do not add brittle tests duplicating every CSS rule or component literal. Existing concurrency/authorization integration tests remain the primary protection for preserved business rules; browser checks verify actual rendering and end-to-end usability.

## 11. Risks and decisions during implementation

1. **Permission regression during read refactor:** the monolithic read currently performs many policy checks. Factor Tx helpers, verify actual resource permissions and fail-closed behaviour, and compare actor scenarios. Removing N+1 work must not remove authorization.
2. **Shared-file conflicts:** the orchestrator reserves the route/response/legacy extraction seams; workers write separate feature files and communicate API changes early. Generation/formatting needs coordination. Do not overwrite each other's templates or unrelated changes.
3. **History privacy vs smooth navigation:** htmx caches and browser bfcache are separate. Verify both in real Chromium after revocation/logout. A full reload on restoration is acceptable when needed for fresh authorization.
4. **Capability UX vs token storage:** raw URLs cannot be reconstructed from digests. Show a new link once, retain only safe metadata, and make replacement explicit. Do not introduce plaintext token retention merely to improve a list page.
5. **Merge data loss:** previews are advisory unless the POST validates a fresh fingerprint and explicit clearing. Keep final merge in the existing transaction with the same account/coalescing/audit rules.
6. **Timeline density:** 10 stands may need a local scroller; phones should choose one stand. Keep semantic access and bounded day ranges rather than shrinking text or building multiple expensive UI modes.
7. **Plain and htmx paths diverging:** centralize full/frame/error/status handling and test both. A JS-disabled form must preserve data and confirmation semantics too.
8. **Legacy feedback/design-system callers:** keep compatibility wrapper names only during migration, make the live routes explicit, and remove duplicate template definitions carefully. Never restore unscoped feedback writes.
9. **Test fixture mutation:** browser actions add synthetic data. Use isolated scenario schemas/fixtures where required and do not depend on an exact worker count after signup. Do not reseed populated rehearsal data without a separate disposable target.

The orchestrator can refine filenames and visual spacing to match repository reality, but the scope, resource permissions, server-state boundary, protected-history behaviour, actual receipt/merge/link semantics, practical page limits, and verification gates above must remain explicit in the delivered implementation.
