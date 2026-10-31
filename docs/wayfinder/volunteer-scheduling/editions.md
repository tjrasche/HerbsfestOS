## Question

How are editions and stand participation maintained, how is the Current Edition changed, and what precisely is copied or retained when preparing a new edition?

Preserve these confirmed constraints:

- The central Edition entity has a PostgreSQL table with only a string-valued year column, used as its key; special labels must be supported without assuming numeric year ordering.
- Major edition-scoped domain roots reference Edition. The shared worker directory and lasting stand identities are explicit global exceptions.
- A stand can participate in many editions, and an edition can contain many stands. An editable association represents each stand's participation in an edition.
- New editions copy stand participation and organizer teams from a chosen source edition, defaulting to the Current Edition.
- Copied organizer teams can then change independently per edition. Persistent volunteer stand memberships remain shared.
- Selecting an edition personally and changing the global Current Edition are separate actions. Only global organizers can select another edition personally. Other users work in the Current Edition.
- Existing volunteers retain their memberships when the Current Edition changes and can sign up at their member stands if those stands participate.

Resolve creation and catalog-administration permissions, how Current Edition state is stored outside the one-column Edition table, personal-selection persistence, label corrections, participation removal, editing of past/future schedules, and safe activation or failure behavior.

Copying stand participation and organizer teams is agreed; copying shifts or other configuration has not been agreed and must be decided explicitly if required.

Follow-up from [Define shifts and assignment rules](https://github.com/tjrasche/HerbsfestOS/issues/4): each edition now needs start and end days. Resolve where those dates belong relative to the earlier one-column Edition-table constraint, and how date changes affect the permitted shift window. Date storage has not yet been decided.

Follow-up from [Define worker profiles and invitation linking](https://github.com/tjrasche/HerbsfestOS/issues/3#issuecomment-6403791630): account for edition-bound stand and shift registration links, expiry at the end of the edition in Europe/Berlin, and retained pending requests after expiry/activation changes. Resolve the consequences of date corrections, participation removal, and Current Edition changes for link validity and authorized request review. New editions use their own registration links; worker-specific account connection links instead have an independent seven-day lifetime.
