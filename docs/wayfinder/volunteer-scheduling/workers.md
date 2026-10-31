## Question

What information identifies a volunteer worker, what exactly may limited directory lookup reveal, and how are invitations and verified login identities linked to the correct existing worker profile?

Preserve these confirmed constraints:

- The directory is shared across stands and editions. Workers may exist without accounts and can be scheduled by organizers.
- Organizers may add workers manually. Stand and shift registration links accept names-only requests without a login; optional ZITADEL account creation comes afterward.
- Global organizers manage every worker profile.
- The Current Edition's stand organizers can create profiles and edit profiles of their stand members; edits affect the shared record across all stands and editions.
- Stand organizers have limited lookup across the shared directory to find existing workers for membership invitations. Full worker details remain limited to their own members.
- Eligible invited volunteers can choose their own shifts. Authentication is handled by ZITADEL and authorization by OpenFGA.

Resolve required and optional fields, duplicate handling, account linking and unlinking, invitation expiry/reissue and acceptance, ownership verification, and the distinction between profile data and identity-provider display data. Make the lookup disclosure and any volunteer access to their own profile explicit.

Do not use a matching display name or an unverified email as proof that a login owns a worker record.
