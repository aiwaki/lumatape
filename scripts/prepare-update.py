#!/usr/bin/env python3
"""Create a bounded Tauri latest.json from an already signed NSIS installer.

Offline only. Never creates keys, builds, uploads, or publishes a release.
Requires the optional release-tool dependency `cryptography` for Ed25519.
The portable ZIP is deliberately not an installable updater artifact.
"""
import argparse
import base64
import datetime as dt
import hashlib
import json
import re
import struct
from pathlib import Path

REPOSITORY = "aiwaki/lumatape"
LIMIT = 256 * 1024 * 1024


def read_regular(path, limit):
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_size > limit:
        raise ValueError(f"Expected a bounded regular file: {path.name}")
    data = path.read_bytes()
    if len(data) > limit:
        raise ValueError(f"File grew beyond its limit: {path.name}")
    return data


def decode(encoded, label):
    try:
        return base64.b64decode(encoded, validate=True)
    except ValueError as error:
        raise ValueError(f"Invalid {label} base64") from error


def verify(payload, public_key, signature):
    try:
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PublicKey
    except ImportError as error:
        raise ValueError("Release verification requires the cryptography Python package") from error
    try:
        public_lines = decode(public_key, "public key").decode("utf-8").splitlines()
        signature_lines = decode(signature, "signature").decode("utf-8").splitlines()
        if len(public_lines) != 2 or len(signature_lines) != 4:
            raise ValueError("Unexpected minisign record shape")
        public = decode(public_lines[1], "public key record")
        signed = decode(signature_lines[1], "signature record")
        global_signature = decode(signature_lines[3], "global signature")
        if len(public) != 42 or public[:2] != b"Ed" or len(signed) != 74 or signed[:2] != b"ED":
            raise ValueError("Expected a prehashed minisign Ed25519 signature")
        if public[2:10] != signed[2:10] or not signature_lines[2].startswith("trusted comment: "):
            raise ValueError("Signature key or comment mismatch")
        key = Ed25519PublicKey.from_public_bytes(public[10:])
        key.verify(signed[10:], hashlib.blake2b(payload).digest())
        key.verify(global_signature, signed[10:] + signature_lines[2][17:].encode("utf-8"))
    except (ValueError, UnicodeError, IndexError) as error:
        raise ValueError("Update signature format is invalid") from error
    except Exception as error:
        raise ValueError("Update signature verification failed") from error


def validate_pe_version(payload, version):
    """Read RT_VERSION structurally; never search arbitrary bytes for a signature."""
    def number(fmt, offset):
        size = struct.calcsize(fmt)
        if offset < 0 or offset + size > len(payload):
            raise ValueError("Truncated PE structure")
        return struct.unpack_from(fmt, payload, offset)[0]

    if len(payload) < 64 or payload[:2] != b"MZ":
        raise ValueError("Expected a Windows PE installer")
    pe = number("<I", 60)
    if pe < 64 or payload[pe:pe+4] != b"PE\0\0":
        raise ValueError("Invalid PE signature")
    section_count = number("<H", pe+6)
    optional_size = number("<H", pe+20)
    optional = pe+24
    magic = number("<H", optional)
    directories = optional + {0x10b: 96, 0x20b: 112}.get(magic, 0)
    if magic not in (0x10b, 0x20b) or not 1 <= section_count <= 96:
        raise ValueError("Invalid PE optional header or sections")
    if optional_size < directories - optional + 24 or number("<I", directories-4) < 3:
        raise ValueError("Missing PE resource directory")
    root_rva = number("<I", directories+16)
    resource_size = number("<I", directories+20)
    if not 16 <= resource_size <= 16 * 1024 * 1024:
        raise ValueError("Resource directory exceeds its limit")
    sections = optional + optional_size

    def file_offset(rva, size):
        for i in range(section_count):
            entry = sections + i*40
            address, raw_size, raw = (number("<I", entry+x) for x in (12, 16, 20))
            delta = rva-address
            if rva and 0 <= delta and delta+size <= raw_size and raw+delta+size <= len(payload):
                return raw+delta
        raise ValueError("PE resource RVA is outside file-backed sections")

    root = file_offset(root_rva, resource_size)
    seen = set()

    def bounded(offset, size):
        if offset < 0 or offset+size > resource_size:
            raise ValueError("Resource tree escaped its directory")
        return root+offset

    def entries(offset):
        if offset in seen:
            raise ValueError("Repeated or cyclic resource directory")
        seen.add(offset)
        at = bounded(offset, 16)
        count = number("<H", at+12) + number("<H", at+14)
        if not 1 <= count <= 4096:
            raise ValueError("Resource entry count exceeds its limit")
        at = bounded(offset+16, count*8)
        return [(number("<I", at+i*8), number("<I", at+i*8+4)) for i in range(count)]

    types = [child for ident, child in entries(0) if ident == 16]  # RT_VERSION
    if len(types) != 1 or not types[0] & 0x80000000:
        raise ValueError("Missing unique PE version resource")
    languages = []
    for _, names in entries(types[0] & 0x7fffffff):
        if not names & 0x80000000:
            raise ValueError("Invalid version name directory")
        for _, leaf in entries(names & 0x7fffffff):
            if leaf & 0x80000000:
                raise ValueError("Invalid version language leaf")
            at = bounded(leaf, 16)
            rva, size = number("<I", at), number("<I", at+4)
            if not 52 <= size <= 65536:
                raise ValueError("Version resource exceeds its limit")
            begin = file_offset(rva, size)
            languages.append(payload[begin:begin+size])
    if not 1 <= len(languages) <= 64:
        raise ValueError("Invalid version language count")
    expected = tuple(map(int, version.split("."))) + (0,)
    for blob in languages:
        length, value_length, kind = struct.unpack_from("<HHH", blob, 0)
        if kind != 0 or value_length != 52 or not 52 <= length <= len(blob):
            raise ValueError("Invalid VS_VERSION_INFO header")
        end = 6
        while end+2 <= min(length, 256) and blob[end:end+2] != b"\0\0":
            end += 2
        if end+2 > min(length, 256) or blob[6:end].decode("utf-16le") != "VS_VERSION_INFO":
            raise ValueError("Invalid version resource key")
        value = (end+2+3) & ~3
        if value+52 > length:
            raise ValueError("Truncated fixed version resource")
        fixed = struct.unpack_from("<13I", blob, value)
        def parts(ms, ls):
            return ms >> 16, ms & 65535, ls >> 16, ls & 65535
        if fixed[0] != 0xfeef04bd or parts(fixed[2], fixed[3]) != expected or parts(fixed[4], fixed[5]) != expected:
            raise ValueError("Signed installer version does not match the release version")


def prepare(installer, signature_file, public_key_file, version, published_at):
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", version):
        raise ValueError("Only canonical stable X.Y.Z versions are supported")
    if any(int(part) > 65535 for part in version.split(".")):
        raise ValueError("Version cannot be represented in a Windows version resource")
    name = f"LumaTape_{version}_x64-setup.exe"
    if Path(installer).name != name:
        raise ValueError(f"Expected immutable installer name: {name}")
    payload = read_regular(installer, LIMIT)
    offset = int.from_bytes(payload[60:64], "little")
    if len(payload) < 64 or payload[:2] != b"MZ" or offset < 64 or payload[offset:offset+4] != b"PE\0\0":
        raise ValueError("Expected a Windows PE installer")
    signature = read_regular(signature_file, 4096).decode("utf-8").strip()
    public_key = read_regular(public_key_file, 4096).decode("utf-8").strip()
    verify(payload, public_key, signature)
    validate_pe_version(payload, version)
    date = dt.datetime.fromisoformat(published_at.replace("Z", "+00:00"))
    if date.tzinfo is None:
        raise ValueError("Publication timestamp must include a timezone")
    manifest = {
        "version": version,
        "pub_date": date.astimezone(dt.timezone.utc).isoformat().replace("+00:00", "Z"),
        "platforms": {"windows-x86_64": {
            "signature": signature,
            "url": f"https://github.com/{REPOSITORY}/releases/download/v{version}/{name}",
        }},
    }
    receipt = {
        "version": version, "repository": REPOSITORY, "target": "windows-x86_64",
        "installer": name, "bytes": len(payload), "sha256": hashlib.sha256(payload).hexdigest(),
        "public_key_sha256": hashlib.sha256(public_key.encode()).hexdigest(),
        "signature_verified": True, "binary_version_verified": True, "published": False,
        "qualification": "Signature, PE version and artifact admission only; installed Windows update/rollback is a separate runtime check.",
    }
    return manifest, receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--installer", type=Path, required=True)
    parser.add_argument("--signature", type=Path, required=True)
    parser.add_argument("--public-key", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--published-at", required=True, help="Deterministic RFC3339 source/release timestamp")
    parser.add_argument("--output", type=Path, required=True, help="New, empty output directory")
    args = parser.parse_args()
    manifest, receipt = prepare(args.installer, args.signature, args.public_key, args.version, args.published_at)
    if args.output.exists():
        raise SystemExit("Output already exists; choose a new directory")
    args.output.mkdir(parents=True)
    data = (json.dumps(manifest, ensure_ascii=False, indent=2) + "\n").encode()
    (args.output / "latest.json").write_bytes(data)
    receipt["latest_json_sha256"] = hashlib.sha256(data).hexdigest()
    (args.output / "update-artifact.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    print(f"Verified local update index: {args.output / 'latest.json'} (not published)")


if __name__ == "__main__":
    main()
