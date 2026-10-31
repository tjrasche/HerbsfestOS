<!-- wayfinder-interview: 2026-10-29; issue: 8 -->

## Progress — prototype ready for human review

Policy interview selected three organizer layouts: day timeline, dense schedule table, and approval-first workspace. Share the same phone-friendly names-only guest registration flow and existing German visual assets. Organizers primarily use desktop; all views remain responsive. Include a confirmed names-and-shifts print view for manual delivery to workers without accounts.

An isolated in-memory browser prototype is prepared locally. Run `python3 scripts/serve-scheduling-prototype.py`, then open `http://127.0.0.1:8090/prototype/volunteer-scheduling?variant=timeline` (also `table` and `inbox`; SSH-forward 8090 if needed). No production routes or services are connected.

Review guest all-or-none request submission, optional account connection after saving, organizer approval/capacity feedback, explicit global self-grants, personal versus global edition controls, disabled participation, old own requests, and print output.

This ticket stays open until concrete prototype feedback selects/corrects the workflow. Artifact is local/uncommitted; no throwaway branch was published without explicit commit authorization.
