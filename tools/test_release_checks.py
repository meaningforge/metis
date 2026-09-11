"""Regression checks for public docs and packaged license validation."""
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class ReleaseChecks(unittest.TestCase):
    def test_prs_run_container_and_release_checks_without_publishing(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        self.assertRegex(workflow, r'(?m)^  pull_request:')
        for job in ('container', 'release-packaging'):
            block = re.search(r'(?ms)^  ' + job + r':\n(.*?)(?=^  \S|\Z)', workflow).group(1)
            # A job-wide event filter must not bypass the checks for PRs.
            self.assertNotRegex(block, r'(?m)^    if:')
            self.assertNotIn('github.event_name', block)
            self.assertIn("needs.detect-engine-execution-impact.outputs.docs_only != 'true'", block)
        self.assertIn('docker build', workflow)
        self.assertIn('release --snapshot --clean --skip=publish', workflow)

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
