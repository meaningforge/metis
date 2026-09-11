# RFC-0045: Multi-Project Runtime Bootstrap

> **Design history:** This RFC preserves its original lifecycle status and rationale.
> Implementation claims describe the recorded design, not a guarantee that every
> proposed API remains current. See [current documentation](../../README.md) for
> supported interfaces. Package paths have been updated for this repository.

- **Status:** Implemented
- **Owners:** Metis maintainers
- **Created:** 2026-08-26
- **Last updated:** 2026-08-26
- **Scope:** runtime configuration, project bootstrap, Catalog assembly, execution resolution, readiness metadata
- **Supersedes:** None

## Summary

Allow one Metis process to load multiple independently configured semantic
projects. A deployment manifest references one or more existing project
manifests; bootstrap loads each project, merges their immutable Catalog
snapshots, and configures the execution resolver with every project binding.

## Motivation

RFC-0044 made `list_projects` part of the primary Agent surface, while
`metis serve --config` could previously bootstrap only one project. Catalog and
execution resolution already use project-keyed maps, but the deployable entry
point could not populate more than one entry. The result was a plural API whose
normal deployment returned exactly one project.

## Design

A traditional project manifest remains valid:

```yaml
project: finance
models:
  - path: ./models/*.ossie.yaml
default_execution_binding: duckdb-local
execution_bindings:
  duckdb-local:
    engine: metis-native
    dialect: DUCKDB
```

A deployment manifest contains only project-manifest references:

```yaml
projects:
  - path: ./projects/finance/metis.yaml
  - path: ./projects/growth/metis.yaml
```

`projects[].path` is relative to the deployment manifest and may be a filesystem
glob. It must match at least one file. Expanded paths are sorted; duplicate
paths and duplicate canonical project IDs fail startup.

Each referenced project manifest keeps ownership of its Ossie model paths,
execution bindings, display metadata, and project identity. Project model paths
remain relative to that project manifest, not to the deployment manifest.

Bootstrap builds each project snapshot independently and then creates one
deterministically digested aggregate snapshot. Resolver, Planner, compiler,
REST, and MCP share that aggregate Catalog and one execution resolver configured
with all project manifests. Project isolation and explicit project selection
remain unchanged; there is no implicit default project.

The public readiness response returns sorted `projects`. For compatibility it
also returns singular `project` when exactly one project is loaded.

## Alternatives

Embedding full project definitions below `projects:` was rejected because it
would couple independently owned semantic domains into one large deployment
file and make relative model ownership unclear.

Running one Metis process per project remains a valid deployment topology, but
does not fulfill the multi-project discovery contract for a shared Agent
endpoint.

## Rollout and migration

Existing project manifests and `metis serve --config project/metis.yaml` remain
valid. Multi-project deployments opt in by pointing `--config` at a deployment
manifest. No semantic model or execution-binding migration is required.

## Test and acceptance criteria

- one deployment manifest loads two project manifests through a relative glob;
- `list_projects` sees both projects in deterministic order;
- both projects compile using their own execution configuration;
- duplicate projects, duplicate paths, unknown fields, empty project lists, and
  zero-match globs fail startup;
- aggregate digest is independent of project load order;
- the existing single-project bootstrap path remains compatible.

## Documentation updates

- `docs/specs/operations/runtime-bootstrap.md`
- `docs/design/operations/runtime-bootstrap.md`
- root `README.md`
