#!/usr/bin/env python3
"""Reject obvious secret files and credential material before packaging source."""

from __future__ import annotations

import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
FORBIDDEN_NAMES = {".env", "gateway.env"}
FORBIDDEN_SUFFIXES = {".bak", ".exe", ".log", ".secret", ".pem", ".p12", ".pfx", ".key"}
FORBIDDEN_ROOTS = {".uwa-testing", "bin", "dist", "reports"}
SECRET_MARKERS = {
    "private_key": re.compile(rb"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----"),
    "github_token": re.compile(rb"gh(?:p|o|u|s|r)_[A-Za-z0-9_]{30,}"),
    "aws_access_key": re.compile(rb"AKIA[0-9A-Z]{16}"),
    "google_api_key": re.compile(rb"AIza[0-9A-Za-z_-]{35}"),
}
ENV_ASSIGNMENT = re.compile(
    rb"(?m)^[ \t]*(OPENWEATHER_API_KEY|WEATHERAPI_API_KEY|LOCATIONIQ_API_KEY|"
    rb"UWA_POSTGRES_PASSWORD|APP_DEV_CLIENT_KEY)[ \t]*=[ \t]*([^\s#]+)"
)
PLACEHOLDER = re.compile(rb"(?i)^(?:\$|\{|<|your|example|replace|changeme|dummy|test|fake|\"?\$)")


def main() -> int:
    raw = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=ROOT
    )
    names = sorted({part.decode("utf-8", "surrogateescape") for part in raw.split(b"\0") if part})
    findings: list[tuple[str, str]] = []
    for name in names:
        relative = pathlib.PurePosixPath(name.replace("\\", "/"))
        if relative.parts[0] in FORBIDDEN_ROOTS or relative.name in FORBIDDEN_NAMES or relative.suffix.lower() in FORBIDDEN_SUFFIXES:
            findings.append((name, "forbidden_release_path"))
            continue
        path = ROOT / name
        if not path.is_file() or path.stat().st_size > 5_000_000:
            continue
        content = path.read_bytes()
        for marker, pattern in SECRET_MARKERS.items():
            if pattern.search(content):
                findings.append((name, marker))
        for match in ENV_ASSIGNMENT.finditer(content):
            if not PLACEHOLDER.match(match.group(2)):
                findings.append((name, "literal_credential_assignment"))
                break
    for name, reason in findings:
        print(f"{name}: {reason}")
    if findings:
        print(f"Release tree check failed: {len(findings)} finding(s).")
        return 1
    print(f"Release tree check passed: {len(names)} tracked/unignored source files checked.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
