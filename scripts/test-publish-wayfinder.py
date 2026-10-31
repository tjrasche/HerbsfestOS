#!/usr/bin/env python3
"""Exercise publication against a simulated GitHub; never contacts the network."""

import contextlib
from copy import deepcopy
import importlib.util
import io
import json
from pathlib import Path
import re
import sys
import unittest
from unittest.mock import patch
from urllib.parse import urlsplit


sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('publish_wayfinder', Path(__file__).with_name('publish-wayfinder.py'))
publisher = importlib.util.module_from_spec(spec)
spec.loader.exec_module(publisher)


class FakeGitHub:
    def __init__(self):
        self.base = '/repos/tjrasche/HerbsfestOS'
        self.issues = {}
        self.labels = {}
        self.children = {}
        self.blockers = {}
        self.calls = []
        self.fail_preflight = False
        self.fail_create_once = None
        self.research_assignee = None

    def collection(self, path):
        return self.call('GET', path)

    def by_id(self, database_id):
        return next(issue for issue in self.issues.values() if issue['id'] == database_id)

    def call(self, method, path, payload=None, paginate=False):
        self.calls.append((method, path, deepcopy(payload)))
        path = urlsplit(path).path
        if path == self.base:
            if self.fail_preflight:
                raise RuntimeError('API connectivity unavailable')
            return {'permissions': {'push': True}}
        if path == '/user':
            return {'login': 'tjrasche'}
        if path == self.base + '/labels':
            if method == 'POST':
                self.labels[payload['name']] = deepcopy(payload)
                return deepcopy(payload)
            return list(deepcopy(self.labels).values())
        if path == self.base + '/issues':
            if method == 'GET':
                return deepcopy(list(self.issues.values()))
            number = max(self.issues, default=0) + 1
            if self.fail_create_once == number:
                self.fail_create_once = None
                raise RuntimeError('Interrupted publication')
            issue = {'id': 10000 + 19 * number, 'number': number,
                     'title': payload['title'], 'body': payload['body'],
                     'html_url': f'https://github.com/tjrasche/HerbsfestOS/issues/{number}',
                     'state': 'open', 'assignees': [],
                     'labels': [{'name': name} for name in payload['labels']]}
            if 'item: research -->' in issue['body'] and self.research_assignee:
                issue['assignees'] = [{'login': self.research_assignee}]
            self.issues[number] = issue
            return deepcopy(issue)
        match = re.fullmatch(re.escape(self.base) + r'/issues/(\d+)(.*)', path)
        if not match:
            raise AssertionError(f'Unexpected request: {method} {path}')
        number, tail = int(match[1]), match[2]
        issue = self.issues[number]
        if not tail:
            if method == 'PATCH':
                issue.update(payload)
            return deepcopy(issue)
        if tail == '/sub_issues':
            children = self.children.setdefault(number, set())
            if method == 'POST':
                self.by_id(payload['sub_issue_id'])
                children.add(payload['sub_issue_id'])
                return deepcopy(self.by_id(payload['sub_issue_id']))
            return [deepcopy(self.by_id(key)) for key in sorted(children)]
        if tail == '/parent':
            parent_number = next(parent for parent, children in self.children.items() if issue['id'] in children)
            return deepcopy(self.issues[parent_number])
        if tail == '/dependencies/blocked_by':
            blockers = self.blockers.setdefault(number, set())
            if method == 'POST':
                self.by_id(payload['issue_id'])
                blockers.add(payload['issue_id'])
                return deepcopy(issue)
            return [deepcopy(self.by_id(key)) for key in sorted(blockers)]
        if tail == '/assignees' and method == 'POST':
            existing = {person['login'] for person in issue['assignees']}
            issue['assignees'] += [{'login': login} for login in payload['assignees'] if login not in existing]
            return deepcopy(issue)
        raise AssertionError(f'Unexpected request: {method} {path}')


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.manifest = publisher.load_packet()
        self.api = FakeGitHub()

    def test_dry_run_never_initializes_github(self):
        output = io.StringIO()
        with patch.object(sys, 'argv', ['publish-wayfinder.py', '--dry-run']), \
                patch.object(publisher, 'GitHub', side_effect=AssertionError('Network not allowed')), \
                contextlib.redirect_stdout(output):
            publisher.main()
        result = json.loads(output.getvalue())
        self.assertEqual(result['tickets'], 8)
        self.assertEqual(result['native_dependencies'], 8)
        self.assertEqual(len(result['initial_frontier']), 3)

    def test_connectivity_failure_causes_no_writes(self):
        self.api.fail_preflight = True
        with self.assertRaisesRegex(RuntimeError, 'connectivity'):
            publisher.publish(self.manifest, self.api)
        self.assertEqual(self.api.calls, [('GET', self.api.base, None)])
        self.assertFalse(self.api.issues)
        self.assertFalse(self.api.labels)

    def test_native_graph_uses_database_ids_and_claims_only_research(self):
        result = publisher.publish(self.manifest, self.api)
        self.assertEqual(len(self.api.issues), 9)
        self.assertEqual(sum(map(len, self.api.children.values())), 8)
        self.assertEqual(sum(map(len, self.api.blockers.values())), 8)
        posts = [(path, body) for method, path, body in self.api.calls if method == 'POST']
        for path, body in posts:
            if path.endswith('/sub_issues'):
                self.assertGreater(body['sub_issue_id'], 10000)
            if path.endswith('/dependencies/blocked_by'):
                self.assertGreater(body['issue_id'], 10000)
        self.assertEqual(result['research']['status'], 'claimed')
        self.assertEqual([issue['title'] for issue in result['frontier']], [
            'Define shifts and assignment rules', 'Define edition and stand lifecycle'])
        self.assertEqual(sum(bool(issue['assignees']) for issue in self.api.issues.values()), 1)
        for issue in self.api.issues.values():
            self.assertEqual(len(issue['labels']), 1)
            if 'item: map -->' not in issue['body']:
                self.assertIn('[Define volunteer scheduling, editions, and OpenFGA authorization]', issue['body'])

    def test_rerun_is_idempotent_and_preserves_human_edits(self):
        publisher.publish(self.manifest, self.api)
        target = next(issue for issue in self.api.issues.values() if 'item: authorization -->' in issue['body'])
        target['body'] += '\nHuman context added after publication.\n'
        map_issue = self.api.issues[1]
        map_issue['body'] += '\nExisting resolution index entry.\n'
        self.api.calls.clear()
        publisher.publish(self.manifest, self.api)
        self.assertFalse([entry for entry in self.api.calls if entry[0] in ('POST', 'PATCH')])
        self.assertIn('Human context added after publication.', target['body'])
        self.assertIn('Existing resolution index entry.', map_issue['body'])

    def test_interrupted_creation_resumes_without_duplicates(self):
        self.api.fail_create_once = 4
        with self.assertRaisesRegex(RuntimeError, 'Interrupted'):
            publisher.publish(self.manifest, self.api)
        self.assertEqual(len(self.api.issues), 3)
        publisher.publish(self.manifest, self.api)
        self.assertEqual(len(self.api.issues), 9)
        self.assertEqual(sum(map(len, self.api.children.values())), 8)
        self.assertEqual(sum(map(len, self.api.blockers.values())), 8)

    def test_closed_research_unblocks_worker_decision(self):
        publisher.publish(self.manifest, self.api)
        research = next(issue for issue in self.api.issues.values() if 'item: research -->' in issue['body'])
        research['state'] = 'closed'
        result = publisher.publish(self.manifest, self.api)
        self.assertEqual(result['research']['status'], 'closed')
        self.assertEqual([issue['title'] for issue in result['frontier']], [
            'Define worker profiles and invitation linking', 'Define shifts and assignment rules',
            'Define edition and stand lifecycle'])

    def test_research_claim_by_another_developer_is_preserved(self):
        self.api.research_assignee = 'another-developer'
        result = publisher.publish(self.manifest, self.api)
        self.assertEqual(result['research']['status'], 'claimed_elsewhere')
        self.assertFalse([entry for entry in self.api.calls if entry[1].endswith('/assignees')])

    def test_unowned_duplicate_title_stops_before_writes(self):
        self.api.issues[1] = {'id': 10019, 'number': 1, 'title': self.manifest['map']['title'],
                              'body': 'An unrelated existing issue', 'labels': []}
        with self.assertRaisesRegex(RuntimeError, 'refusing to duplicate'):
            publisher.publish(self.manifest, self.api)
        self.assertFalse([entry for entry in self.api.calls if entry[0] in ('POST', 'PATCH')])


if __name__ == '__main__':
    unittest.main()
