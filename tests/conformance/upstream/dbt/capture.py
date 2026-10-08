#!/usr/bin/env python3
"""Capture native dbt artifacts and acceptance evidence; never convert Ossie."""
import argparse
import hashlib
import importlib.metadata
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

import duckdb
import yaml

HERE = Path(__file__).resolve().parent
PINS = {'dbt-core': '1.12.0', 'dbt-duckdb': '1.11.0', 'duckdb': '1.5.5', 'metricflow': '0.213.0'}


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(command, project, environment, log):
    result = subprocess.run(command, cwd=project, env=environment, capture_output=True, text=True, timeout=120)
    log.write_text(result.stdout + result.stderr)
    events = []
    for line in result.stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get('info', {}).get('level') in ('warn', 'error'):
            events.append(event)
    return result.returncode, events


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--output', type=Path, required=True, help='new private evidence directory')
    parser.add_argument('--metis', type=Path, required=True, help='actual built Metis CLI')
    args = parser.parse_args()
    versions = {name: importlib.metadata.version(name) for name in PINS}
    if versions != PINS:
        raise SystemExit(f'version drift: {versions}, expected {PINS}')
    output = args.output.resolve()
    if args.output.exists() or args.output.is_symlink():
        raise SystemExit('output already exists')
    os.umask(0o077)
    output.mkdir(mode=0o700)
    metis = str(args.metis.resolve(strict=True))
    base = yaml.safe_load((HERE / 'cases/basic.yml').read_text())
    dbt = str(Path(sys.executable).parent / 'dbt')
    if not Path(dbt).is_file():
        raise SystemExit('dbt executable is required in the pinned environment')
    evidence = {'versions': versions, 'python_version': sys.version.split()[0],
                'cases': {}, 'scope': 'native artifacts; no converter or publication'}
    for case in ('basic', 'window', 'visibility', 'invalid'):
        project = output / case
        shutil.copytree(HERE / 'project', project)
        native = json.loads(json.dumps(base))
        orders, customers = native['models']
        if case == 'window':
            orders['metrics'] += [
                {'name': 'rolling_revenue', 'type': 'cumulative', 'input_metric': 'revenue', 'window': '3 days'},
                {'name': 'monthly_revenue', 'type': 'cumulative', 'input_metric': 'revenue', 'grain_to_date': 'month'},
            ]
        if case == 'visibility':
            orders['metrics'] += [{'name': 'private_revenue', 'type': 'simple', 'agg': 'sum', 'expr': 'amount', 'hidden': True}]
            # SCD models cannot also declare primary/unique entities upstream.
            customers['columns'][0].pop('entity')
            customers['primary_entity'] = 'customer'
            customers['columns'] += [
                {'name': 'customer_code', 'data_type': 'varchar', 'entity': {'name': 'customer', 'type': 'natural'}},
                {'name': 'valid_from', 'data_type': 'timestamp', 'granularity': 'day', 'dimension': {'type': 'time', 'validity_params': {'is_start': True, 'is_end': False}}},
                {'name': 'valid_to', 'data_type': 'timestamp', 'granularity': 'day', 'dimension': {'type': 'time', 'validity_params': {'is_start': False, 'is_end': True}}},
            ]
        if case == 'invalid':
            orders['metrics'][0]['agg'] = 'not_an_aggregation'
        source = project / 'models/semantic.yml'
        source.write_text(yaml.safe_dump(native, sort_keys=False))
        environment = dict(os.environ, DBT_NATIVE_DATABASE_PATH=str(project / 'demo.duckdb'),
                           DBT_SEND_ANONYMOUS_USAGE_STATS='false', DBT_USE_COLORS='false')
        command = [dbt, 'parse', '--project-dir', str(project), '--profiles-dir', str(project),
                   '--no-partial-parse', '--log-format', 'json']
        code, diagnostics = run(command, project, environment, project / 'parse.jsonl')
        case_evidence = {'dbt_exit': code, 'diagnostics': diagnostics, 'source_sha256': digest(source)}
        if case == 'window' and code != 0:
            # dbt1.12.0's new YAML parser writes legacy window params; capture
            # the default failure, then the documented explicit compatibility
            # flag. This is native dbt behavior, not a Metis transformation.
            case_evidence['default_parse'] = {'exit': code, 'diagnostics': diagnostics}
            config_path = project / 'dbt_project.yml'
            config = yaml.safe_load(config_path.read_text())
            config['flags'] = {'require_nested_cumulative_type_params': False}
            config_path.write_text(yaml.safe_dump(config, sort_keys=False))
            code, diagnostics = run(command, project, environment, project / 'compatibility-parse.jsonl')
            case_evidence['compatibility_flag'] = config['flags']
            case_evidence['dbt_exit'], case_evidence['diagnostics'] = code, diagnostics
        target = project / 'target'
        artifact = target / 'osi_document.json'
        if code == 0 and artifact.is_file():
            case_evidence['osi_sha256'] = digest(artifact)
            # Same-invocation native semantic manifest stays beside the Ossie artifact.
            case_evidence['semantic_manifest_sha256'] = digest(target / 'semantic_manifest.json')
            first = project / 'native-first'
            first.mkdir()
            shutil.copy2(artifact, first / artifact.name)
            shutil.copy2(target / 'semantic_manifest.json', first / 'semantic_manifest.json')
            shutil.copy2(source, first / 'semantic.yml')
            native_artifact = json.loads(artifact.read_text())
            case_evidence['osi_version'] = native_artifact['version']
            validation = subprocess.run([metis, 'model', 'validate', '--model', str(artifact)],
                                        capture_output=True, text=True, timeout=30)
            case_evidence['metis_exit'] = validation.returncode
            case_evidence['metis_diagnostic'] = validation.stdout + validation.stderr
            case_evidence['metis_compile'] = 'NOT_EXECUTED' if validation.returncode else 'NOT_REQUESTED'
            code_again, again_events = run(command, project, environment, project / 'repeat.jsonl')
            case_evidence['repeat_exit'] = code_again
            case_evidence['repeat_osi_equal'] = code_again == 0 and digest(artifact) == case_evidence['osi_sha256']
            case_evidence['repeat_diagnostics'] = again_events
            if case == 'basic':
                build = list(command)
                build[1] = 'run'
                build_code, build_events = run(build, project, environment, project / 'build.jsonl')
                case_evidence['dbt_build_exit'] = build_code
                case_evidence['dbt_build_diagnostics'] = build_events
                if build_code == 0:
                    connection = duckdb.connect(str(project / 'demo.duckdb'), read_only=True)
                    rows = connection.execute('SELECT region, SUM(amount) FROM orders JOIN customers USING(customer_id) GROUP BY region ORDER BY region').fetchall()
                    connection.close()
                    actual = [[region, str(amount)] for region, amount in rows]
                    assert actual == [['APAC', '120.00'], ['EMEA', '80.00']], actual
                    case_evidence['independent_fixture_rows'] = actual
                # Demonstrate stale-artifact risk in the SAME target after a valid parse.
                stale_hash = digest(artifact)
                broken = json.loads(json.dumps(native))
                broken['models'][0]['metrics'][0]['agg'] = 'not_an_aggregation'
                source.write_text(yaml.safe_dump(broken, sort_keys=False))
                stale_code, stale_events = run(command, project, environment, project / 'failed-after-success.jsonl')
                case_evidence['failed_after_success'] = {
                    'exit': stale_code, 'diagnostics': stale_events,
                    'old_osi_still_present': artifact.is_file(),
                    'old_osi_unchanged': artifact.is_file() and digest(artifact) == stale_hash,
                }
                (project / 'failed-source.yml').write_text(source.read_text())
                source.write_text(yaml.safe_dump(native, sort_keys=False))
        else:
            case_evidence['osi_present'] = artifact.is_file()
            case_evidence['metis_compile'] = 'NOT_EXECUTED'
        evidence['cases'][case] = case_evidence
    evidence['native_parser_version'] = importlib.metadata.version('dbt-core-experimental-parser')
    (output / 'acceptance.json').write_text(json.dumps(evidence, indent=2) + '\n')
    print(output / 'acceptance.json')
    for name in ('basic', 'window', 'visibility'):
        case = evidence['cases'][name]
        assert case['dbt_exit'] == 0 and case.get('repeat_osi_equal'), (name, case)
    assert evidence['cases']['invalid']['dbt_exit'] != 0
    assert not evidence['cases']['invalid']['osi_present']
    stale = evidence['cases']['basic']['failed_after_success']
    assert stale['exit'] != 0 and stale['old_osi_unchanged']
    issues = lambda name: {event.get('data', {}).get('issue_type') for event in evidence['cases'][name]['diagnostics']}
    assert 'CUMULATIVE_SEMANTICS_LOSS' in issues('window')
    assert {'PRIVATE_METRIC_DROPPED', 'NATURAL_ENTITY_DROPPED'} <= issues('visibility')


if __name__ == '__main__':
    main()
