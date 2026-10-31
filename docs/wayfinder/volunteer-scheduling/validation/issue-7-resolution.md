<!-- wayfinder-interview: 2026-10-29; issue: 7 -->

## Resolution — migration and existing-route access

Agreed in the live interview on 2026-10-29.

- Use a planned maintenance window with paused application traffic, tested backup/restore, versioned transactional migration, and authorization validation before reopening.
- Backfill existing feedback notes to one explicitly owner-selected edition with explicitly supplied festival dates. Never guess from CreatedAt or whichever edition happens to be Current. Preserve IDs, titles, timestamps and counts. Missing inputs or failed validation prevent writes/reopening. Keep migration independent of OpenFGA availability.
- Enforce edition references on major domain roots; shared worker directory and lasting stands remain global. Shifts/requests/assignments derive edition from immutable parent ownership; organizer teams belong to edition participation. Copies never inherit requests/assignments/history.
- Feedback listing and creation require global organizer access for the actual selected edition. Design-system examples become public and contain no private data. Audit every private route when replacing club-member admission; login alone never grants internal information. Existing public/auth/static/operational endpoints retain their public purpose.
- Bootstrap one explicitly identified login operationally, not everyone holding the old club-member role. Validate isolation, missing-input behavior, migration reruns and rollback on disposable data. After new scoped writes, use forward recovery rather than blindly reverting to the unscoped old application.

The detailed cutover and acceptance runbook is prepared locally. No production data was read or migrated.
