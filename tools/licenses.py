#!/usr/bin/env python3
"""Reproduce the license bundle for the supported command build matrix."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(system, arch) for system in ('linux', 'darwin') for arch in ('amd64', 'arm64')]


def go(*args, env=None):
    return subprocess.check_output(['go', *args], cwd=ROOT, env=env, text=True)


def objects(text):
    decoder = json.JSONDecoder()
    while text.strip():
        text = text.lstrip()
        value, end = decoder.raw_decode(text)
        yield value
        text = text[end:]


def module_dependencies():
    modules = {}
    for system, arch in TARGETS:
        for duckdb in (False, True):
            env = dict(os.environ, GOOS=system, GOARCH=arch,
                       CGO_ENABLED='1' if duckdb else '0', GOFLAGS='', GOWORK='off')
            args = ['list', '-mod=readonly', '-deps', '-json']
            if duckdb:
                args += ['-tags', 'duckdb']
            args += ['./cmd/...']
            for package in objects(go(*args, env=env)):
                module = package.get('Module')
                if not module or module.get('Main'):
                    continue
                if module.get('Replace'):
                    raise RuntimeError('Release dependency uses a replace directive: ' + module['Path'])
                modules[module['Path']] = module
    return [modules[key] for key in sorted(modules)]


def escape_module(path):
    return ''.join('!' + ch.lower() if ch.isupper() else ch for ch in path)


def collect():
    files = {}
    records = []
    for module in module_dependencies():
        name, version = module['Path'], module['Version']
        directory = Path(module['Dir'])
        prefix = f'licenses/_modules/{name}@{version}'
        legal = []
        # Preserve nested notices as well as the top-level license. Some Go
        # modules include third-party code under subdirectories.
        for path in sorted(directory.rglob('*')):
            if not path.is_file() or path.suffix.lower() in {'.go', '.mod', '.sum', '.py', '.js', '.ts', '.sh'}:
                continue
            relative = path.relative_to(directory)
            if re.match(r'(?i)^(licen[cs]e|notice|copying|copyright)([._-].*)?$', path.name) or any(part.lower() == 'licenses' for part in relative.parts[:-1]):
                target = f'{prefix}/{relative.as_posix()}'
                files[target] = path.read_bytes()
                legal.append(target)
        if not legal:
            raise RuntimeError('No license text found for ' + name)
        # The module proxy zip is the exact distributed module source at the
        # version selected by go.mod, including MPL-covered MySQL driver files.
        source = f'https://proxy.golang.org/{escape_module(name)}/@v/{version}.zip'
        records.append(dict(module=name, version=version, source=source, notices=legal))
    goroot = Path(go('env', 'GOROOT').strip())
    go_license = next((path for path in (goroot / 'LICENSE', goroot.parent / 'LICENSE') if path.is_file()), None)
    if go_license is None:
        raise RuntimeError('Go toolchain LICENSE is missing')
    files['licenses/go/LICENSE'] = go_license.read_bytes()
    files['licenses/components.json'] = (json.dumps(records, indent=2) + '\n').encode()
    intro = '''Third-party notices for Metis
============================

Metis is licensed under Apache-2.0. Third-party components retain their own
licenses; this file does not relicense them. The verbatim license, copyright,
and notice texts are distributed under licenses/.

This bundle covers the Go module dependencies of commands under cmd/ for
Linux and macOS, amd64 and arm64, both default and DuckDB-enabled builds.
It is a union: an individual executable may use only a subset.
Regenerate with: python3 tools/licenses.py --write
Verify against the current dependencies with: python3 tools/licenses.py --check

Go runtime and standard library: BSD-style license in licenses/go/LICENSE.
Source: https://go.dev/dl/ (select the Go version reported by metis version).

Apache Ossie: commit 88e0011148283302c9a04cd0287e00e0b9d87354, Apache-2.0.
The generated Go bindings are derived from its schema. See NOTICE and
licenses/ossie/ for the unchanged upstream license and notice.

Go MySQL Driver (github.com/go-sql-driver/mysql) is licensed under MPL-2.0.
Its unmodified source is available at the exact versioned source URL below.
The MPL-covered source remains available under MPL-2.0; Metis's Apache-2.0
license does not restrict recipients' rights to that source.

External agent CLIs, database servers, and extensions downloaded at runtime
are installed separately and retain their own licenses. When redistributing
those components or generated benchmark bundles, retain their applicable
license and attribution materials as well.

Go module sources and notices
-----------------------------
'''
    text = intro
    for record in records:
        text += f"\n{record['module']} {record['version']}\nSource: {record['source']}\n"
        text += ''.join('  ' + path + '\n' for path in record['notices'])
    files['THIRD_PARTY_NOTICES'] = text.encode()
    for name in ('LICENSE', 'NOTICE', 'licenses/ossie/LICENSE', 'licenses/ossie/NOTICE'):
        files[name] = (ROOT / name).read_bytes()
    hashes = ''.join(f'{hashlib.sha256(data).hexdigest()}  {name}\n' for name, data in sorted(files.items()))
    files['licenses/SHA256SUMS'] = hashes.encode()
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument('--write', action='store_true')
    mode.add_argument('--check', action='store_true')
    args = parser.parse_args()
    files = collect()
    existing = {p.relative_to(ROOT).as_posix() for p in (ROOT / 'licenses').rglob('*') if p.is_file()}
    stale = existing - files.keys()
    changed = [name for name, data in files.items() if not (ROOT / name).is_file() or (ROOT / name).read_bytes() != data]
    if args.check:
        if stale or changed:
            sys.exit('License bundle is stale; run python3 tools/licenses.py --write.\n' + '\n'.join(sorted(stale | set(changed))))
    else:
        for name in stale:
            (ROOT / name).unlink()
        for name, data in files.items():
            path = ROOT / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
    print(f'License bundle: PASS ({len(files)} files)')


if __name__ == '__main__':
    main()
