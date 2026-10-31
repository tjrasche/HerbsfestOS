#!/usr/bin/env python3
"""Publish the agreed packet through native GitHub issue relationships.

Uses existing gh authentication. --dry-run only validates local inputs.
API references: https://docs.github.com/en/rest/issues/sub-issues
and https://docs.github.com/en/rest/issues/issue-dependencies.
"""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[1]
PACKET = ROOT / 'docs/wayfinder/volunteer-scheduling'
CONTEXT_START = '<!-- wayfinder-context:start -->'
CONTEXT_END = '<!-- wayfinder-context:end -->'
LABEL_COLORS = {
    'wayfinder:map': '735cdd',
    'wayfinder:research': '0075ca',
    'wayfinder:grilling': 'e4e669',
    'wayfinder:prototype': 'a2eeef',
}


def load_packet():
    manifest = json.loads((PACKET / 'manifest.json').read_text())
    if manifest['repository'] != 'tjrasche/HerbsfestOS':
        raise ValueError('This packet is authorized only for tjrasche/HerbsfestOS')
    items = [manifest['map'], *manifest['tickets']]
    by_key = {item['key']: item for item in items}
    if len(by_key) != len(items) or len({item['title'] for item in items}) != len(items):
        raise ValueError('Packet keys and titles must be unique')
    for item in items:
        if item['label'] not in LABEL_COLORS:
            raise ValueError(f"Unsupported label: {item['label']}")
        path = (PACKET / item['body_file']).resolve()
        if path.parent != PACKET.resolve():
            raise ValueError('Body files must belong to the publication packet')
        item['body'] = path.read_text().strip() + '\n'
        if item['key'] != 'map' and not item['body'].startswith('## Question\n'):
            raise ValueError(f"Missing question: {item['title']}")
        if re.search(r'#<[^>]+>|#\{[^}]+\}', item['body']):
            raise ValueError('Issue-number placeholders must not be published')
        for key in item.get('blocked_by', []) + item.get('related', []):
            if key not in by_key or key == item['key'] or key == 'map':
                raise ValueError(f"Invalid reference in {item['title']}: {key}")
    visiting, visited = set(), set()

    def visit(key):
        if key in visiting:
            raise ValueError('Blocking relationships contain a cycle')
        if key in visited:
            return
        visiting.add(key)
        for blocker in by_key[key].get('blocked_by', []):
            visit(blocker)
        visiting.remove(key)
        visited.add(key)

    for key in by_key:
        visit(key)
    return manifest


class GitHub:
    def call(self, method, path, payload=None, paginate=False):
        command = ['gh', 'api', '--hostname', 'github.com', '--method', method,
                   '-H', 'Accept: application/vnd.github+json',
                   '-H', 'X-GitHub-Api-Version: 2026-03-10', path]
        if paginate:
            command += ['--paginate', '--slurp']
        if payload is not None:
            command += ['--input', '-']
        result = subprocess.run(command, input=None if payload is None else json.dumps(payload),
                                text=True, capture_output=True, check=False)
        if result.returncode:
            raise RuntimeError(f'GitHub {method} {path} failed: {result.stderr.strip()}')
        value = json.loads(result.stdout) if result.stdout.strip() else None
        if paginate:
            if not isinstance(value, list) or any(not isinstance(page, list) for page in value):
                raise RuntimeError(f'Unexpected paginated response: {path}')
            return [item for page in value for item in page]
        return value

    def collection(self, path):
        separator = '&' if '?' in path else '?'
        return self.call('GET', path + separator + 'per_page=100', paginate=True)


def marker(manifest, item):
    return f"<!-- wayfinder-packet: {manifest['packet']}; item: {item['key']} -->"


def issue_link(issue):
    return f"[{issue['title']}]({issue['html_url']})"


def linked_body(body, item, nodes):
    lines = [CONTEXT_START, 'Map: ' + issue_link(nodes['map'])]
    keys = list(dict.fromkeys(item.get('blocked_by', []) + item.get('related', [])))
    if keys:
        lines += ['', 'Related decisions: ' + ', '.join(issue_link(nodes[key]) for key in keys) + '.']
    lines += [CONTEXT_END]
    block = '\n'.join(lines)
    if body.count(CONTEXT_START) != body.count(CONTEXT_END) or body.count(CONTEXT_START) > 1:
        raise RuntimeError(f"Malformed generated context block: {item['title']}")
    if CONTEXT_START in body:
        start = body.index(CONTEXT_START)
        end = body.index(CONTEXT_END, start) + len(CONTEXT_END)
        return body[:start] + block + body[end:]
    return body.rstrip() + '\n\n' + block + '\n'


def publish(manifest, api):
    repo = '/repos/' + manifest['repository']
    # This is deliberately the first remote call: no writes after a failed preflight.
    permissions = api.call('GET', repo).get('permissions', {})
    if not any(permissions.get(key) for key in ('admin', 'maintain', 'push', 'triage')):
        raise RuntimeError('Repository issue-write permissions are required')
    login = api.call('GET', '/user')['login']
    items = [manifest['map'], *manifest['tickets']]
    existing = [issue for issue in api.collection(repo + '/issues?state=all')
                if 'pull_request' not in issue]
    nodes = {}
    for item in items:
        owned = [issue for issue in existing if marker(manifest, item) in (issue.get('body') or '')]
        collisions = [issue for issue in existing
                      if issue['title'] == item['title'] and issue not in owned]
        if len(owned) > 1 or collisions:
            raise RuntimeError(f"Ambiguous existing issue; refusing to duplicate: {item['title']}")
        if owned:
            nodes[item['key']] = owned[0]

    labels = {label['name'] for label in api.collection(repo + '/labels')}
    for label in sorted({item['label'] for item in items} - labels):
        api.call('POST', repo + '/labels', {'name': label, 'color': LABEL_COLORS[label]})
    for item in items:
        if item['key'] not in nodes:
            nodes[item['key']] = api.call('POST', repo + '/issues', {
                'title': item['title'], 'body': marker(manifest, item) + '\n\n' + item['body'],
                'labels': [item['label']],
            })

    # Native hierarchy and dependencies are a second pass, using database IDs.
    parent_path = repo + f"/issues/{nodes['map']['number']}"
    children = {issue['id'] for issue in api.collection(parent_path + '/sub_issues')}
    for item in manifest['tickets']:
        issue = nodes[item['key']]
        if issue['id'] not in children:
            api.call('POST', parent_path + '/sub_issues', {'sub_issue_id': issue['id']})
        path = repo + f"/issues/{issue['number']}"
        blockers = {entry['id'] for entry in api.collection(path + '/dependencies/blocked_by')}
        for key in item['blocked_by']:
            blocker = nodes[key]
            if blocker['id'] not in blockers:
                api.call('POST', path + '/dependencies/blocked_by', {'issue_id': blocker['id']})
        # Preserve human edits outside the publisher's owned context-link block.
        latest = api.call('GET', path)
        body = linked_body(latest.get('body') or '', item, nodes)
        if body != latest.get('body'):
            api.call('PATCH', path, {'body': body})

    native_children = {issue['id'] for issue in api.collection(parent_path + '/sub_issues')}
    blockers_by_key = {}
    for item in items:
        issue = api.call('GET', repo + f"/issues/{nodes[item['key']]['number']}")
        nodes[item['key']] = issue
        if {label['name'] for label in issue['labels']} != {item['label']}:
            raise RuntimeError(f"Unexpected labels; preserving them for review: {issue['title']}")
        if item['key'] == 'map':
            continue
        if issue['id'] not in native_children:
            raise RuntimeError(f"Native parent relationship missing: {issue['title']}")
        path = repo + f"/issues/{issue['number']}"
        parent = api.call('GET', path + '/parent')
        if parent['id'] != nodes['map']['id']:
            raise RuntimeError(f"Unexpected parent: {issue['title']}")
        blockers = api.collection(path + '/dependencies/blocked_by')
        expected = {nodes[key]['id'] for key in item['blocked_by']}
        if not expected.issubset({entry['id'] for entry in blockers}):
            raise RuntimeError(f"Native blocking relationship missing: {issue['title']}")
        blockers_by_key[item['key']] = blockers

    research = next(item for item in manifest['tickets'] if item['label'] == 'wayfinder:research')
    issue = nodes[research['key']]
    assignees = {person['login'] for person in issue['assignees']}
    research_status = 'closed' if issue['state'] == 'closed' else 'claimed_elsewhere'
    open_research_blockers = [entry for entry in blockers_by_key[research['key']]
                             if entry['state'] == 'open']
    if issue['state'] == 'open' and open_research_blockers:
        research_status = 'blocked'
    elif issue['state'] == 'open' and not assignees:
        issue = api.call('POST', repo + f"/issues/{issue['number']}/assignees", {'assignees': [login]})
        nodes[research['key']] = issue
        assignees = {person['login'] for person in issue['assignees']}
        research_status = 'claimed' if assignees == {login} else 'claim_conflict'
    elif issue['state'] == 'open' and assignees == {login}:
        research_status = 'claimed'

    frontier = [nodes[item['key']] for item in manifest['tickets']
                if nodes[item['key']]['state'] == 'open'
                and not nodes[item['key']]['assignees']
                and not any(entry['state'] == 'open' for entry in blockers_by_key[item['key']])]
    return {
        'map': {'title': nodes['map']['title'], 'url': nodes['map']['html_url']},
        'tickets': [{'key': item['key'], 'title': nodes[item['key']]['title'],
                     'url': nodes[item['key']]['html_url']} for item in manifest['tickets']],
        'frontier': [{'title': issue['title'], 'url': issue['html_url']} for issue in frontier],
        'research': {'status': research_status, 'title': issue['title'],
                     'url': nodes[research['key']]['html_url'], 'assignee': login,
                     'branch': manifest['research_branch']},
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    manifest = load_packet()
    if args.dry_run:
        keys = {item['key']: item['title'] for item in manifest['tickets']}
        print(json.dumps({
            'repository': manifest['repository'], 'map': manifest['map']['title'],
            'tickets': len(manifest['tickets']),
            'native_dependencies': sum(len(item['blocked_by']) for item in manifest['tickets']),
            'initial_frontier': [item['title'] for item in manifest['tickets'] if not item['blocked_by']],
            'dependencies': {item['title']: [keys[key] for key in item['blocked_by']]
                             for item in manifest['tickets'] if item['blocked_by']},
        }, indent=2))
        return
    digest = hashlib.sha256((manifest['repository'] + manifest['packet']).encode()).hexdigest()[:16]
    fd = os.open('/tmp/wayfinder-publish-' + digest + '.lock', os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise RuntimeError('Another local publisher is already running for this packet') from error
        print(json.dumps(publish(manifest, GitHub()), indent=2))


if __name__ == '__main__':
    try:
        main()
    except (ValueError, KeyError, RuntimeError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
