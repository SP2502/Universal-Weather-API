#!/usr/bin/env python3
"""Create a deterministic source ZIP from the reviewed Git release tree."""

from __future__ import annotations

import hashlib
import pathlib
import subprocess
import sys
import zipfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
VERSION = sys.argv[1] if len(sys.argv) > 1 else "0.1.0-rc1"
if not VERSION or any(ch not in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-" for ch in VERSION):
    raise SystemExit("Version must contain only letters, numbers, dots and hyphens")

subprocess.run([sys.executable, str(ROOT / "scripts/check_release_tree.py")], cwd=ROOT, check=True)
raw = subprocess.check_output(
    ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=ROOT
)
names = sorted({part.decode("utf-8", "surrogateescape") for part in raw.split(b"\0") if part})
out_dir = ROOT / "dist"
out_dir.mkdir(exist_ok=True)
archive = out_dir / f"uwa-v{VERSION}-source.zip"
temporary = out_dir / f"uwa-v{VERSION}-source.zip.part"
with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as bundle:
    for name in names:
        rel = pathlib.PurePosixPath(name.replace("\\", "/"))
        path = ROOT / name
        if rel.is_absolute() or ".." in rel.parts or not path.is_file() or path.is_symlink():
            raise SystemExit(f"Unsafe release path: {name}")
        entry = zipfile.ZipInfo(str(pathlib.PurePosixPath(f"uwa-v{VERSION}") / rel))
        entry.date_time = (1980, 1, 1, 0, 0, 0)
        entry.compress_type = zipfile.ZIP_DEFLATED
        entry.external_attr = (0o755 if rel.suffix == ".sh" else 0o644) << 16
        bundle.writestr(entry, path.read_bytes(), compress_type=zipfile.ZIP_DEFLATED, compresslevel=9)
temporary.replace(archive)
digest = hashlib.sha256(archive.read_bytes()).hexdigest()
(out_dir / "SOURCE-SHA256SUMS").write_text(f"{digest}  {archive.name}\n", encoding="ascii")
print(f"Source archive: {archive}")
print(f"Included files: {len(names)}; SHA-256: {digest}")
