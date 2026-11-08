"""Reject private working material in Git and enforce the release manifest."""
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parent.parent
DOCS = {
    "api.md", "benchmarks.md", "extensions.md", "openapi.json",
    "operations.md", "reference.md", "release_notes.md", "verification.md",
}
EXAMPLES = {
    "api.sh", "bootstrap.yaml", "config.json", "docker/init-roles.sql",
    "docker/manage.yaml", "docker/permissions.sql", "docker/serve.yaml",
}
ROOT_FILES = {
    ".dockerignore", ".env.example", ".gitignore", "Dockerfile", "LICENSE",
    "Makefile", "README.md", "compose.yaml", "getting_started.md", "go.mod", "go.sum",
}
SOURCE_DIRS = {".github", "cmd", "internal", "scripts", "tests"}
SECRET = re.compile(rb"(?m)^-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|(?:ghp_|gho_|github_pat_)[A-Za-z0-9_]{20,}|AKIA[A-Z0-9]{16}")


def allowed(name, archive=False):
    path = PurePosixPath(name)
    if path.is_absolute() or ".." in path.parts:
        return False
    parts = path.parts
    if not parts:
        return True
    if any(part in {".local", ".demo", ".agents", ".codex", ".aws", "__pycache__"} for part in parts):
        return False
    if name in {"AGENTS.md", "CLAUDE.md"}:
        return not archive
    if path.name.lower() in {"plan.md", "agents.md", "claude.md", "research.md", "review.md"} or "prompt" in path.name.lower():
        return False
    if path.suffix in {".txt", ".jsonl", ".prof", ".pprof", ".trace", ".log", ".out", ".pyc", ".pem", ".key", ".p12", ".pfx"}:
        return False
    if path.name.startswith(".env") and name != ".env.example":
        return False
    if parts[0] == "docs":
        return len(parts) == 2 and parts[1] in DOCS
    if parts[0] == "examples":
        return str(PurePosixPath(*parts[1:])) in EXAMPLES
    if archive:
        return len(parts) == 1 and parts[0] in {"routeqtus", "LICENSE", "README.md", "getting_started.md"}
    return name in ROOT_FILES or parts[0] in SOURCE_DIRS


def check_archive(filename):
    seen = set()
    with tarfile.open(filename) as archive:
        for member in archive.getmembers():
            name = member.name.removeprefix("./")
            if member.isdir():
                continue
            if not member.isfile() or not allowed(name, archive=True):
                raise ValueError(f"Unapproved release entry: {name}")
            seen.add(name)
            if name != "routeqtus":
                data = archive.extractfile(member).read()
                if SECRET.search(data):
                    raise ValueError(f"Possible secret material in release: {name}")
    expected = {"routeqtus", "LICENSE", "README.md", "getting_started.md"}
    expected.update("docs/" + name for name in DOCS)
    expected.update("examples/" + name for name in EXAMPLES)
    if seen != expected:
        raise ValueError(f"Incomplete release manifest: {filename}")


def main():
    assert allowed("docs/openapi.json") and allowed("examples/config.json")
    for name in ("AGENTS.md", "CLAUDE.md"):
        assert allowed(name), name
        assert not allowed(name, archive=True), name
    for name in ("PLAN.md", "routeqtus_prompt.md", "internal/AGENTS.md", "docs/CLAUDE.md", "agents.md", "claude.md", ".local/AGENTS.md", "../AGENTS.md", ".env", ".local/notes.md", "docs/run.txt", "docs/data.jsonl", "docs/review.md"):
        assert not allowed(name), name
        assert not allowed(name, archive=True), name
    paths = subprocess.check_output(["git", "ls-files", "-z"], cwd=ROOT).decode().split("\0")
    for name in filter(None, paths):
        if not allowed(name):
            raise ValueError(f"Unapproved tracked file: {name}")
        path = ROOT / name
        if path.is_file() and SECRET.search(path.read_bytes()):
            raise ValueError(f"Possible secret material in tracked file: {name}")
        if path.suffix == ".md":
            for target in re.findall(r"\[[^\]]*\]\(([^)]+)\)", path.read_text()):
                target = target.split("#", 1)[0]
                if not target or "://" in target or target.startswith("mailto:"):
                    continue
                resolved = (path.parent / target).resolve()
                if not resolved.is_file() or not allowed(resolved.relative_to(ROOT).as_posix()):
                    raise ValueError(f"Missing or private documentation link in {name}: {target}")
    for filename in sys.argv[1:]:
        check_archive(filename)
    print("Publication manifest passed.")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, tarfile.TarError) as error:
        raise SystemExit(str(error)) from error
