# Volunteer scheduling publication packet

This directory contains the agreed wayfinder map and eight decision-ticket bodies for `tjrasche/HerbsfestOS`. They are publication inputs; the canonical map is [Define volunteer scheduling, editions, and OpenFGA authorization](https://github.com/tjrasche/HerbsfestOS/issues/1).

`manifest.json` records labels, ordering, related context, and native blocking edges. Confirmed domain inputs live on the relevant ticket; the detailed authorization baseline lives in `authorization.md`.

Validate without accessing GitHub:

```sh
python3 scripts/publish-wayfinder.py --dry-run
```

Publish using the host's existing GitHub CLI authentication:

```sh
python3 scripts/publish-wayfinder.py
```

The publisher checks CLI API access before any remote write, creates missing labels and issues, then adds native sub-issue and blocking relationships using database IDs. It reuses this packet's marked issues on retries, preserves edits outside its generated context-link block, and validates the graph before claiming the research ticket. It never substitutes body conventions for native relationships.

Publication is complete. On 2026-10-25, the host's GitHub CLI access and all eight native sub-issue and blocking relationships were verified. Consult the live map and tickets for current claims and resolutions; the packet retains the initial questions.

[Research identity and OpenFGA integration options](https://github.com/tjrasche/HerbsfestOS/issues/2) is resolved. Its cited note is published on the throwaway `research/identity-openfga-integration` branch; no PR was opened. [Define shifts and assignment rules](https://github.com/tjrasche/HerbsfestOS/issues/4) and [Define worker profiles and invitation linking](https://github.com/tjrasche/HerbsfestOS/issues/3) are also resolved. The shift resolution includes the approval-policy amendment from the worker interview. Consult their resolution comments and the canonical map rather than treating this packet's initial questions as the final decisions.

The subsequent live interview resolved [Define edition and stand lifecycle](https://github.com/tjrasche/HerbsfestOS/issues/5), [Define and validate OpenFGA authorization](https://github.com/tjrasche/HerbsfestOS/issues/6), and [Define edition migration and existing-route access](https://github.com/tjrasche/HerbsfestOS/issues/7). Their resolution comments and the live map are canonical. [Prototype organizer and volunteer scheduling](https://github.com/tjrasche/HerbsfestOS/issues/8) is resolved after human MVP approval; [Agree MVP acceptance and first-use plan](https://github.com/tjrasche/HerbsfestOS/issues/9) is resolved: the criteria and pilot plan are agreed. The canonical map is complete and closed; production implementation and actual pilot validation follow this planning effort. See [the isolated prototype, model checks, migration runbook, and rehearsal fixture](validation/README.md). No application implementation or deployment has been performed.

Native API references: [sub-issues](https://docs.github.com/en/rest/issues/sub-issues), [issue dependencies](https://docs.github.com/en/rest/issues/issue-dependencies).
