#!/usr/bin/env python3
"""Check local Markdown links while allowing public project documentation."""
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

root = Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else Path(__file__).resolve().parents[1]
for required in ('README.md', 'LICENSE'):
    if not (root / required).is_file() or not (root / required).stat().st_size:
        sys.exit(f'Missing or empty required file: {required}')

excluded = {'.git', 'bin', 'dist', 'node_modules', 'vendor', 'licenses', 's2sbench-results', '.workload', '.workload.partial'}
errors = []
count = 0
for doc in sorted(root.rglob('*.md')):
    if any(part in excluded for part in doc.relative_to(root).parts):
        continue
    count += 1
    fence = None
    for number, line in enumerate(doc.read_text().splitlines(), 1):
        marker = re.match(r'^\s*(`{3,}|~{3,})', line)
        if marker:
            if fence is None:
                fence = marker[1]
            elif marker[1][0] == fence[0] and len(marker[1]) >= len(fence):
                fence = None
            continue
        if fence:
            continue
        # Strip code spans, then inspect inline/image links and reference definitions.
        line = re.sub(r'(`+).*?\1', '', line)
        destinations = re.findall(r'\]\(\s*(<[^>]+>|[^\s)]+)', line)
        reference = re.match(r'^\s{0,3}\[[^]]+\]:\s*(<[^>]+>|\S+)', line)
        if reference:
            destinations.append(reference[1])
        for destination in destinations:
            url = urlsplit(destination.strip('<>'))
            if url.scheme or url.netloc or not url.path:
                continue
            target = root / unquote(url.path).lstrip('/') if url.path.startswith('/') else doc.parent / unquote(url.path)
            if not target.exists():
                errors.append(f'{doc.relative_to(root)}:{number}: missing local link: {destination}')
if errors:
    sys.exit('\n'.join(errors))
print(f'Documentation links: PASS ({count} Markdown files)')
