## Question

Which concrete ZITADEL invitation APIs and OpenFGA mechanisms support the agreed scheduling policy, and what identity, context, relationship-write, and consistency guarantees must the application account for?

Investigate against primary documentation and the current repository:

- Invited-user onboarding without requiring the ZITADEL club-member role; stable verified identity and safe linking to pre-existing worker profiles.
- OpenFGA relationships for persistent stand membership, edition-specific organizer teams, stand participation, and inherited shift permissions.
- Trusted Current Edition context versus personal edition selection; changing the global default must not require re-inviting every existing volunteer.
- Tuple creation, reading, deletion, model pinning, authorization after grants/revocations, and failure or partial-update behavior.
- The integration work implied by the existing client, which currently exposes Ping and Check but neither installs models nor writes relationships.

Treat the confirmed product policy in **Define and validate OpenFGA authorization** as an input. Surface unsupported assumptions and alternatives without choosing new product policy.

Capture one cited research note on a throwaway research branch, link it from this ticket, and open no PR. Claim this ticket before starting. Do not inspect production credentials or mutate the live identity or authorization deployment.
