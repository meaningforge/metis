"""Regression checks for public docs and packaged license validation."""
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

from ci.change_scope import classify

ROOT = Path(__file__).resolve().parents[1]


class ReleaseChecks(unittest.TestCase):
    def test_prs_keep_checks_and_select_packaging_without_publishing(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        self.assertRegex(workflow, r'(?m)^  pull_request:')
        for job in ('container', 'release-packaging'):
            block = re.search(r'(?ms)^  ' + job + r':\n(.*?)(?=^  \S|\Z)', workflow).group(1)
            # Preserve the check contexts, including a successful no-op package job.
            self.assertNotRegex(block, r'(?m)^    if:')
            self.assertNotIn('github.event_name', block)
            output = 'docs_only' if job == 'container' else 'packaging_required'
            self.assertIn(f'needs.detect-engine-execution-impact.outputs.{output}', block)
        self.assertIn('docker build', workflow)
        self.assertIn('release --snapshot --clean --skip=publish,before', workflow)
        self.assertIn('git diff --name-only -z', workflow)
        self.assertIn('python3 tools/ci/change_scope.py', workflow)
        self.assertIn('make check', workflow)
        self.assertIn('make test-duckdb-backend', workflow)
        self.assertIn('make test-e2e', workflow)
        self.assertNotIn('cache: false', workflow)

    def test_change_scope_keeps_required_coverage(self):
        for event in ('pull_request', 'push'):
            self.assertEqual(classify(event, ['README.md', 'docs/guide.md']), (True, False))
            # An empty/unknown diff is conservative, not a docs-only shortcut.
            self.assertEqual(classify(event, []), (False, True))
        ordinary = ['app/tooling/authoring/input.go', 'cmd/metis/project_init_command.go',
                    'tests/conformance/scenarios/metrics.go', 'examples/demo/project.yaml']
        for path in ordinary:
            self.assertEqual(classify('pull_request', [path]), (False, False))
            self.assertEqual(classify('push', [path]), (False, True))
        critical = ['.goreleaser.yml', 'Dockerfile', 'go.mod', 'go.sum', 'Makefile',
                    'LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES',
                    'licenses/_modules/example/LICENSE.md',
                    'tools/ci/change_scope.py', 'tools/test_release_checks.py',
                    '.github/workflows/ci.yml', 'app/tooling/authoring/publish_darwin.go',
                    'execution/driver/runtime_linux_amd64.go']
        for path in critical:
            self.assertEqual(classify('pull_request', [path, 'docs/guide.md']), (False, True), path)
        self.assertEqual(classify('workflow_dispatch', ['README.md']), (False, True))

    def test_scope_cli_handles_git_paths_without_interpolation(self):
        script = ROOT / 'tools/ci/change_scope.py'
        result = subprocess.run([sys.executable, str(script), '--event', 'pull_request'],
                                input=b'app/unusual\nname.go\0README.md\0', capture_output=True)
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout, b'docs_only=false\npackaging_required=false\n')

    def test_tag_release_keeps_gate_before_skipping_duplicate_hooks(self):
        workflow = (ROOT / '.github/workflows/release.yml').read_text()
        self.assertIn('go mod tidy', workflow)
        self.assertIn('git diff --exit-code -- go.mod go.sum', workflow)
        self.assertIn('make release-check', workflow)
        self.assertIn('args: release --clean --skip=before', workflow)
        self.assertLess(workflow.index('make release-check'), workflow.index('--skip=before'))
        # Direct GoReleaser use still retains the original safety hooks.
        config = (ROOT / '.goreleaser.yml').read_text()
        self.assertIn('go test ./...', config)
        self.assertIn('python3 tools/licenses.py --check', config)

    def test_public_documents_and_relative_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'LICENSE').write_text('test license')
            (root / 'README.md').write_text('[Guide](docs/guide.md) [Contribute](CONTRIBUTING.md)')
            (root / 'CONTRIBUTING.md').write_text('[Home](README.md)')
            (root / 'SECURITY.md').write_text('Report security issues to the maintainers.')
            (root / 'docs').mkdir()
            guide = root / 'docs/guide.md'
            guide.write_text('[Home](../README.md)\n```md\n[Example](not-a-file)\n```\n')
            command = [sys.executable, str(ROOT / 'tools/check_docs.py'), str(root)]
            self.assertEqual(subprocess.run(command, capture_output=True).returncode, 0)
            guide.write_text('[Home](../README.md) [Broken](missing.md)')
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('docs/guide.md:1: missing local link: missing.md', result.stderr)

    def test_packaged_notices_must_be_present_and_unchanged(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES'):
                shutil.copy2(ROOT / name, root / name)
            shutil.copytree(ROOT / 'licenses', root / 'licenses')
            command = ['bash', str(ROOT / 'tools/ci/license-bundle-check.sh'), str(root)]
            self.assertEqual(subprocess.run(command, capture_output=True).returncode, 0)
            (root / 'NOTICE').unlink()
            self.assertNotEqual(subprocess.run(command, capture_output=True).returncode, 0)
            shutil.copy2(ROOT / 'NOTICE', root / 'NOTICE')
            license_file = next((root / 'licenses/_modules').rglob('LICENSE*'))
            license_file.write_text('altered license')
            self.assertNotEqual(subprocess.run(command, capture_output=True).returncode, 0)


if __name__ == '__main__':
    unittest.main()
