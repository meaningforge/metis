"""Classify changed paths for CI; consume NUL-delimited git diff output."""
import argparse
from pathlib import PurePosixPath
import re
import sys


PACKAGING_FILES = {
    ".goreleaser.yml", ".goreleaser.yaml", "Dockerfile", ".dockerignore",
    "go.mod", "go.sum", "Makefile", "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES",
    "tools/licenses.py", "tools/test_release_checks.py",
}
PLATFORM_FILE = re.compile(
    r"_(?:linux|darwin|windows|unix|freebsd|openbsd|netbsd|android|ios|"
    r"amd64|arm64|386|arm|ppc64|ppc64le|riscv64|s390x)(?:_test)?\.go$"
)


def classify(event, paths):
    packaging = any(
        path in PACKAGING_FILES
        or path.startswith(("licenses/", "tools/ci/"))
        or (path.startswith(".github/") and not path.endswith(".md"))
        or PLATFORM_FILE.search(PurePosixPath(path).name)
        for path in paths
    )
    docs_only = bool(paths) and not packaging and all(
        path.startswith("docs/") or path.endswith(".md") for path in paths
    )
    if event == "workflow_dispatch" or not paths:
        return False, True
    if docs_only:
        return True, False
    if event == "push":
        return False, True
    return False, packaging


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--event", required=True,
                        choices=("pull_request", "push", "workflow_dispatch"))
    args = parser.parse_args()
    raw = sys.stdin.buffer.read()
    paths = [path.decode("utf-8", errors="surrogateescape")
             for path in raw.split(b"\0") if path]
    docs_only, packaging = classify(args.event, paths)
    print(f"docs_only={str(docs_only).lower()}")
    print(f"packaging_required={str(bool(packaging)).lower()}")


if __name__ == "__main__":
    main()
