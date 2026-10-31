<!-- wayfinder-interview: 2026-10-29; issue: 6 -->

## Resolution — authorization policy and validation

Agreed in the live interview on 2026-10-29. Existing worker and shift resolutions remain authoritative.

- PostgreSQL owns accounts anchored to validated issuer/subject, optional one-to-one worker connections, memberships and organizer grants. An organizer can later volunteer using a worker record connected to the same login; no second account is needed.
- Use fresh, server-derived contextual facts per OpenFGA request for direct grants, ownership, Current Edition and participation state. Do not project live grants asynchronously or retain them in login sessions. Stable hierarchy may be contextual too. Pin the model and use stable opaque object identities independent of edition renames.
- Schedule view: global OR enabled/current/team-or-member. Edit/approval: enabled AND explicit team AND current-or-global. Global visibility does not imply editing. Disabled schedules are global-readonly. Only globals may browse another edition. The browser never supplies authorization gates.
- Connected workers read their own old/disabled requests but cannot withdraw them. Withdrawal requires an own pending request in enabled Current Edition participation. Members see names/assignments, not others' contacts or notes. Existing masking and own-worker information rules remain.
- Global organizers copy/share bearer organizer invitations. Acceptance by the first authenticated redeemer grants exactly the invitation scope, without recipient-email binding. Single-use; seven-day expiry; explicit revoke/replace; opening alone does not consume. An issuer losing global access invalidates outstanding invitations, not already accepted grants. Protect the last global organizer.
- Removing a grant/membership or DB-maintained worker connection must affect the next request. Coordinate authorization snapshots and database mutation commits; keep assignment plus membership atomic and enforce hard capacity transactionally. OpenFGA errors stop protected work including guest submissions. Public pages and sign-in remain available.
- Use hosted ZITADEL registration and optional authenticated provider linking, with automatic email/name linking disabled. Application bearer proof and authenticated issuer/subject establish worker ownership, never contact matching.

A schema 1.1 model and disposable OpenFGA v1.21.0 validation runner are prepared locally. Engine checks cover global/self-grant distinction, edition/participation gates, guest capability isolation, worker privacy, read-only archived requests and next-request revocation. Application context/transaction tests remain implementation acceptance work, not a claim of existing integration.
