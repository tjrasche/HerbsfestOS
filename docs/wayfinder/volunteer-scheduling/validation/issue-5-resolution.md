<!-- wayfinder-interview: 2026-10-29; issue: 5 -->

## Resolution — edition and stand lifecycle

Agreed in the live Wayfinder interview on 2026-10-29.

- Edition retains its editable string year key and gains start/end date columns. Global organizers alone create editions/stands, correct dates/shared stand names, and manage participation. Renames preserve every reference; opaque authorization identity remains stable.
- Date changes save with warnings and retain shifts outside the revised ±14-day window. Flagged shifts remain usable under normal request/approval rules. New shifts and time edits must fit the revised window. No automatic shift movement.
- New editions copy only enabled stand participation and organizer teams from the chosen source, defaulting to Current Edition. Teams then vary independently. Schedules start empty; no shifts, requests, assignments or registration links are copied. Shared worker memberships persist.
- Disabling participation retains teams, schedule, requests, history and memberships. No new requests/assignments or schedule edits; globals alone see the retained schedule read-only. Reactivation restores eligibility under current rules. Own old/disabled requests remain read-only under the authorization decision.
- Current Edition changes only by explicit global action, including switching back; never by calendar rollover. Require valid dates, at least one enabled stand, and organizers for every enabled stand. Preserve those requirements after activation. Failed or competing switches leave one unambiguous effective edition, preserving the previous one on failure.
- The first edition starts empty and is activated explicitly after preparation; until then ordinary users see scheduling unavailable. Personal browsing follows Current Edition until explicitly pinned for the browser session. Selecting Current Edition resumes following. Selection never grants permissions.
- Registration links follow corrected dates, so an extension can make a date-expired link valid again; disabled/replaced links stay invalid. Membership and submitted requests survive switches. Old links never target a copied edition.
- Entire edition or lasting-stand deletion remains owner-operated database maintenance.

Validation handoff and prototype are prepared locally; no production application change or deployment is part of this resolution.
