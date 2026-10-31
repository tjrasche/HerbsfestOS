## Question

How are existing feedback notes and new domain roots assigned to editions, and what explicit OpenFGA permissions protect existing routes after the admission gate changes?

The repository currently persists only FeedbackNote with ID, Title, and CreatedAt, migrates it with GORM AutoMigrate, and lists notes without edition scoping. Private routes share a club-member gate. Invited volunteer access will make that old route-level assumption insufficient.

Resolve the target edition for existing rows, initialization and rollback/error behavior, edition references and isolation for each root, and the permissions for viewing/creating existing feedback notes. Preserve the agreed global worker directory and lasting stand identities.

Review all existing routes so replacing the club-member admission gate does not silently grant every invited volunteer access to previously internal information. Describe compatibility and migration acceptance scenarios without assuming the contents of production data or accessing credentials.
