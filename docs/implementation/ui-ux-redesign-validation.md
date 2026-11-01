# UI/UX redesign verification

Date: 2026-11-01. Implementation and verification complete. The final rebuilt application passed the complete browser, role and outage matrix on the same binary used for Go verification. The detailed [implementation plan](ui-ux-redesign.md) records the intended scope and contracts.

## Implemented behavior

- Responsive German workspace and guest shells, one persistent edition selector, real active navigation/breadcrumbs, dedicated reusable templ components, and full/frame rendering through `#app-frame`.
- Server-filtered stand/day timeline with bounded summaries, named stand columns, proportional start positions, overlap/presentation lanes, midnight continuation labels and focused shift details. Request queues and own requests are separate paginated destinations.
- Separate edition, participation/team, stand, access and invitation screens; guided setup uses the existing activation predicates. Persistent stand membership remains separate from edition participation/team assignment.
- Paginated privacy-aware worker search, one contextual assignment/membership chooser, permission-aware profiles, preserved shared notes, and fresh merge comparisons with explicit contact choices/clearing.
- Non-bearer capability management, one-time absolute link presentation from the configured origin, explicit HTTP replacement confirmations, optional proof identity/scope, and canonical names-only signup receipts including retries.
- Contextual success/error responses preserve inputs on 422/409. Real htmx pending buttons, visible skeletons, abort/network cleanup and fresh history restoration work without a client domain store. Ordinary links/forms remain usable without JavaScript.
- Confirmed-only compact print tables repeat stand/edition/day identity across pages. Public landing and component-reference pages retain actual content in both full and frame responses.

There is no application schema migration, authorization-model change, infrastructure/configuration change, provider setting change, deployment, automatic scheduling, bulk import or email/SMS delivery in this redesign. No agent committed or published changes.

## Go verification

Executed on this host with the existing disposable loopback PostgreSQL/OpenFGA services. The integration helper creates isolated schemas and fresh FGA stores; the database tests actually ran rather than skipping.

```sh
PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH go tool templ generate

PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH \
  SCHEDULING_TEST_DATABASE_URL='postgres://tjark@127.0.0.1:55432/scheduling_test?sslmode=disable' \
  SCHEDULING_TEST_FGA_URL=http://127.0.0.1:58180 \
  go test ./...

PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH go vet ./...
PATH=/tmp/herbstfest-tools/go/bin:/tmp/herbstfest-tools/bin:$PATH \
  go build -o /tmp/herbstfest-ux-redesign/web ./cmd/web
git diff --check
```

Generation, all-package tests, vet and build passed. The orchestrator all-package run executed scheduling tests in 6.096 seconds; the final root `-count=1` run executed them in 6.212 seconds. Both used the disposable database/FGA environment. Existing capacity, atomic request/assignment, revocation, migration and authorization-race tests remain intact. Root also ran the seven publishing/manifest tests with Kustomize on PATH; all passed. Added checks cover full/frame/history headers and disclosure, field/value preservation, scoped read costs, edition/stand context mapping, persistent membership-search context/direct actions, safe continuations/origins/returns, DST gaps/repeated hours, midnight/short-shift geometry, queue-page recovery, masking/pagination, omitted notes, merge freshness/clearing, non-bearer management, scoped receipt retry and six full/frame replacement-confirmation cases.

The first full run caught a compatibility `ui.Layout` wrapper that lost templ children. It was corrected, public landing was brought into the same frame contract, and dedicated reference-page full/frame/content tests plus the complete suite passed afterward. A cold review then found deep-list navigation retaining scroll and membership search losing its originating stand. Route/pagination navigation now reveals the heading while live search preserves scroll/focus/selection; persistent `member_stand` context now reaches a direct, permission-checked row action. Targeted tests and the full suite passed after those fixes. An independent cold reviewer confirmed all three follow-up findings resolved at 1440px and 390px: heading visible at scroll zero, persistent membership context/direct action/already-member state, and humane length copy.

## Browser run

Use only disposable loopback applications. The browser suite submits synthetic signup, edit, link and setup forms. The empty-role fixture must have no editions before each complete setup run. The old `scripts/test-scheduling-browser.py` delegates to the new runner.

```sh
SCHEDULING_BROWSER_URL=http://127.0.0.1:58081 \
SCHEDULING_BROWSER_OUTPUT=/tmp/herbstfest-ui-redesign \
SCHEDULING_BROWSER_EMPTY_URL=http://127.0.0.1:58082 \
SCHEDULING_BROWSER_MEMBER_URL=http://127.0.0.1:58083 \
SCHEDULING_BROWSER_STAND_URL=http://127.0.0.1:58084 \
  /tmp/wayfinder-validation/browser-env/bin/python scripts/test-ui-redesign-browser.py
```

Optional `SCHEDULING_BROWSER_OUTAGE_URL` points to a separate disposable preview configured with an unreachable loopback FGA endpoint. This tests safe 503 replacement without stopping the shared engine. Leave optional role/outage URLs unset when those scenarios are not prepared.

The global preview uses `scheduling_ui_rehearsal`, a fresh restored synthetic fixture and a fresh local FGA model, with `APP_BASE_URL=http://127.0.0.1:58081`. Root-owned empty/member/stand/outage previews use separate databases and trusted grants/bindings. The existing preview on 58080 and shared services were left intact. PostgreSQL client binaries are under `/tmp/herbstfest-tools/pg/usr/lib/postgresql/18/bin` and require `LD_LIBRARY_PATH=/tmp/herbstfest-tools/pg/usr/lib/x86_64-linux-gnu` on this host.

The final combined run passed on the same rebuilt binary at ports 58081–58085:

- Ten direct page destinations; actual delayed loading/skeleton/busy state; same-document navigation and freshly authorised back/forward history, with no local/session htmx HTML snapshots.
- 422/409 input preservation and a successful contextual edit; 25-row paging, latest-search abort/focus retention, interrupted GET cleanup and unknown-outcome POST cleanup.
- One-time absolute signup link followed by non-bearer digest management; directory-to-profile heading visible at both 1440px and 390px.
- Actual membership flow: create a worker at one stand, search from another stand's membership list with persistent `member_stand`, add directly from the result, retain query/context, show the already-member badge and verify the persistent membership page.
- No document overflow at 320px, 390px or 768px; names-only two-shift guest submission and receipt; visible public gallery content and privacy; normal no-JavaScript validation fallback.
- Empty first setup through edition → stand → participation → explicit self-team → activation. Restricted stand/member roles received the correct denials; a separate unreachable-FGA preview rendered safe 503 content.
- A4 PDF generation and confirmed-only printing; a 15-shift output used two pages with repeated edition/stand/day identity and no contacts or pending rows.

There were zero JavaScript runtime errors. Local screenshots and `roster.pdf` are in `/tmp/herbstfest-ui-redesign`; these are temporary synthetic validation artifacts. The final browser summary reported three forms and 17,710 bytes for the default schedule.

## Measurements

Local measurements describe the synthetic fixture and loopback services; they are not production latency guarantees.

| Measurement | Result |
| --- | --- |
| Real browser fixture | 10 stands, 150 shifts, more than 250 synthetic workers |
| Final default response | 5 chosen stand/day summaries, 17,710 uncompressed HTML bytes, 3 forms including logout/edition/filter |
| Warm default GET latency | 20 sequential reads; p95 97.20 ms, range 62.82–100.30 ms |
| Instrumented read fixture | 10 stands × 15 same-day shifts |
| Chosen stand read | 15 summaries, SQL 80, FGA 32 |
| Explicit all-stand read | Bounded 100 summaries with next page, SQL 80, FGA 32 |
| Summary hydration | Exactly one aggregate query; no per-shift roster/pending lookup or worker catalog/contact hydration |

The latency sample was taken before subsequent synthetic form edits (its response was 17,708 bytes); the final browser measurement was 17,710 bytes. The constant SQL/FGA counts include the existing revision/account-binding reads and per-participation trusted permission facts. Query cost did not grow with visible shift count. Detail loads one roster and at most ten embedded pending requests; queue/own pages use twenty rows; directory/membership/feedback/invitation pages use twenty-five plus one lookahead row.

## Limits

Development mode supplies one fixed synthetic identity; role fixtures change trusted database facts, never browser-selected roles. Public signup and optional public authentication stay anonymous in development mode. A real hosted ZITADEL login/account-registration journey was not performed in browser validation. Existing OIDC tests and new proof-context/redemption tests cover the server boundaries; provider settings and credentials were not inspected or changed. A human organiser/volunteer pilot remains separate from this synthetic verification.
