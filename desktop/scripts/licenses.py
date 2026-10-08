#!/usr/bin/env python3
"""Collect full license texts for the locked Windows Rust dependency graph, offline.

Run after cargo fetch/build has populated the registry. Missing texts are reported
and fail by default; --allow-missing is for auditing, never a release shortcut.
"""
import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path

NAMES = re.compile(r"^(?:licen[cs]e|copying|copyright|unlicense|notice)(?:[._-].*)?$", re.I)
SKIP = {"target", ".git", "node_modules", "examples", "tests", "test", "benches"}
MAX_TEXT = 2 * 1024 * 1024


def license_files(root, declared=None):
    paths = set()
    if declared:
        path = root / declared
        if path.is_file() and path.resolve().is_relative_to(root.resolve()):
            paths.add(path)
    # Crates can ship multiple license variants or vendor texts under LICENSES/.
    def visit(directory, depth):
        if depth > 4:
            return
        for path in sorted(directory.iterdir()):
            if path.is_symlink():
                continue
            if path.is_dir() and path.name not in SKIP:
                visit(path, depth + 1)
            elif path.is_file() and (NAMES.match(path.name) or any(
                p.lower() in {"licenses", "licences"} for p in path.relative_to(root).parts[:-1]
            )):
                paths.add(path)
    visit(root, 0)
    return sorted(paths)


def collect(metadata, destination, overrides=None):
    if overrides is None:
        overrides = Path(__file__).with_name("license-overrides")
    packages = {p["id"]: p for p in metadata["packages"]}
    resolve = metadata.get("resolve") or {}
    nodes = {n["id"]: n for n in resolve.get("nodes", [])}
    todo = list(metadata["workspace_members"])
    reachable = set()
    while todo:
        identity = todo.pop()
        if identity in reachable:
            continue
        reachable.add(identity)
        todo.extend(d["pkg"] for d in nodes.get(identity, {}).get("deps", []))
    records = []
    for identity in sorted(reachable, key=lambda k: (packages[k]["name"], packages[k]["version"])):
        package = packages[identity]
        if identity in metadata["workspace_members"]:
            continue  # Product LICENSE is packaged separately.
        root = Path(package["manifest_path"]).parent
        label = f'{package["name"]}-{package["version"]}'
        record = {"name": package["name"], "version": package["version"],
                  "source": package.get("source"), "license": package.get("license"),
                  "repository": package.get("repository"), "texts": [], "warnings": []}
        if (package.get("source") or "").startswith("registry+"):
            record["source_archive"] = f'https://static.crates.io/crates/{package["name"]}/{label}.crate'
        sources = [(path, root) for path in license_files(root, package.get("license_file"))]
        supplement = overrides / label
        if not sources and supplement.is_dir():
            provenance = json.loads((supplement / "provenance.json").read_text())
            vcs = json.loads((root / ".cargo_vcs_info.json").read_text())
            if provenance["crate"] != label or provenance["commit"] != vcs["git"]["sha1"]:
                raise ValueError(f"License supplement does not match published source: {label}")
            for item in provenance["sources"]:
                path = supplement / item["file"]
                if not path.resolve().is_relative_to(supplement.resolve()):
                    raise ValueError(f"Unsafe license supplement path: {label}")
                if hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"]:
                    raise ValueError(f"License supplement checksum mismatch: {label}")
                sources.append((path, supplement))
            record["supplemental_provenance"] = provenance
            (destination / label).mkdir(parents=True, exist_ok=True)
            (destination / label / "SOURCE-PROVENANCE.json").write_text(json.dumps(provenance, indent=2) + "\n")
        for source, source_root in sources:
            relative = source.relative_to(source_root)
            data = source.read_bytes()
            if len(data) > MAX_TEXT or b"\0" in data:
                record["warnings"].append(f"Unsupported license text: {relative.as_posix()}")
                continue
            try:
                data.decode("utf-8")
            except UnicodeDecodeError:
                record["warnings"].append(f"Non-UTF8 license text retained: {relative.as_posix()}")
            output = destination / label / relative
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_bytes(data)
            record["texts"].append({"path": output.relative_to(destination).as_posix(),
                                    "sha256": hashlib.sha256(data).hexdigest()})
        if not record["texts"]:
            record["warnings"].append("No full license text found in downloaded package")
        records.append(record)
    missing = [f'{r["name"]}-{r["version"]}' for r in records if not r["texts"]]
    return {"schema_version": 1, "target": "x86_64-pc-windows-msvc",
            "offline": True, "package_count": len(records), "missing_texts": missing,
            "packages": records}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=Path(__file__).resolve().parents[1] / "src-tauri/Cargo.toml")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--cargo", default="cargo")
    parser.add_argument("--allow-missing", action="store_true")
    args = parser.parse_args()
    # Only create a new directory: never erase a caller-owned destination.
    if args.output.exists() and any(args.output.iterdir()):
        parser.error("output must be absent or empty; use a fresh staging directory")
    run = subprocess.run([args.cargo, "metadata", "--manifest-path", str(args.manifest),
                          "--locked", "--offline", "--format-version", "1",
                          "--filter-platform", "x86_64-pc-windows-msvc"],
                         text=True, capture_output=True)
    if run.returncode:
        raise SystemExit("Offline Cargo metadata failed; fetch/build locked dependencies first:\n" + run.stderr)
    args.output.mkdir(parents=True, exist_ok=True)
    report = collect(json.loads(run.stdout), args.output)
    report["cargo_lock_sha256"] = hashlib.sha256(args.manifest.with_name("Cargo.lock").read_bytes()).hexdigest()
    (args.output / "manifest.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    lines = ["LumaTape Rust dependency notices", "", "Full available license texts accompany this index.",
             "The SPDX declaration is not a replacement for the included license texts.", ""]
    for package in report["packages"]:
        lines.append(f'{package["name"]} {package["version"]}: {package["license"] or "undeclared"}')
        if package.get("source_archive"):
            lines.append("  Unmodified source: " + package["source_archive"])
        lines.extend("  " + t["path"] for t in package["texts"])
        lines.extend("  WARNING: " + warning for warning in package["warnings"])
    (args.output / "THIRD-PARTY-NOTICES.txt").write_text("\n".join(lines) + "\n")
    print(json.dumps({"packages": report["package_count"], "missing_texts": report["missing_texts"],
                      "output": str(args.output)}, ensure_ascii=False))
    if report["missing_texts"] and not args.allow_missing:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
