## Question

What precise OpenFGA model, object checks, relationship ownership, grant-management workflows, activation behavior, and reconciliation rules implement the confirmed policy below?

ZITADEL authenticates the user. OpenFGA controls application access and action permissions, replacing the current club-member admission gate.

| Capability | Global organizer | Stand organizer | Volunteer |
|---|---|---|---|
| View schedules | Every stand and edition | Assigned stands in Current Edition | Member stands in Current Edition, including worker names and assignments |
| Edit schedules and approve requests | Requires an explicit team grant for that stand and edition | Assigned stands in Current Edition | Submit requests only; no schedule-edit permission |
| Manage stand organizer teams | Yes, including explicit self-grants | No | No |
| Manage persistent stand memberships | Every stand | Their stands in Current Edition | No |
| Manage worker information | Every worker | Create workers and edit their stand members' shared information | View own information/notes; edit own name and contacts |
| Find other workers | Full directory | Search by name/exact contacts; nonmember results show names and masked contacts | No shared-directory lookup |
| Merge duplicate workers | Yes, preserving a sole login and blocking conflicting logins | No | No |
| Select another edition personally | Yes | No | No |
| Change the global Current Edition | Yes | No | No |
| Grant global-organizer access | Yes; initial grant established operationally | No | No |

Preserve the following distinctions:

- Persistent volunteer membership belongs to the lasting stand. Organizer-team grants belong to stand participation in a particular edition.
- Only global organizers may browse another edition. A stored organizer-team grant alone does not bypass the Current Edition restriction.
- Global status alone does not allow schedule editing. A global organizer can deliberately grant themselves the relevant stand-team membership.
- Current Edition changes and personal browsing are separate. Selecting an edition never grants permission by itself.
- Volunteer requests remain restricted to participating stands in the Current Edition. Every request requires organizer approval, including requests from existing members. Approval/direct assignment requires schedule-edit permission.
- Workers may submit names-only requests through valid public registration links without a login. Private worker-resource access uses the verified linked identity; connecting an account is optional and occurs after submission or through a worker-specific private link.
- A stand must participate in the relevant edition; membership in a lasting stand does not make absent participation exist.

Determine derived permissions on actual resources rather than implementing role-name checks throughout handlers. Resolve model/object identifiers, Current Edition context, relationship writes and their source of truth, copying organizer grants, revocation, deletion, retries/reconciliation, bootstrap, and any final directory-field boundaries from **Define worker profiles and invitation linking**.

The existing Go client has Ping and Check, distinguishes denials from operational errors, and pins a configured model ID. It is not wired into application access checks and has no tuple-write/read operations. Keep failures from granting access.

Validate the model against global visibility without editing, explicit self-grants, own versus other stands, past/future edition restrictions, persistent memberships across activation, forged edition requests, revocation, directory disclosure, and unavailable or unconfigured OpenFGA.

Follow-up from [Define worker profiles and invitation linking](https://github.com/tjrasche/HerbsfestOS/issues/3#issuecomment-6403791630): resolve authorization for anonymous stand/shift registration capabilities, limited signup information without rosters, pending-request access, and private account connection after submission or through a single-use seven-day worker link. Also cover own-name/contact editing and notes visibility, global duplicate merging, hosted ZITADEL email/social registration, and effective worker-access revocation after operator-only database changes to a login association. A public registration link or matching name/email cannot claim an existing worker, and identity-provider account creation alone grants no resource access. Determine relationship ownership, proof validation, provider account-linking settings, and failure/reconciliation behavior without requiring a login for public signup.
