#!/usr/bin/env python3
"""Emit a source dependency SPDX document; not binary attestation or rights proof."""
import datetime
import hashlib
import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parents[1]
raw = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=root, text=True)
decoder = json.JSONDecoder()
modules = []
while raw.strip():
    module, end = decoder.raw_decode(raw.lstrip())
    modules.append(module)
    raw = raw.lstrip()[end:]
identity = hashlib.sha256((root / "go.mod").read_bytes() + (root / "go.sum").read_bytes()).hexdigest()
packages, relationships = [], []
for index, module in enumerate(modules):
    ident = "SPDXRef-Package" + str(index)
    package = {"name": module["Path"], "SPDXID": ident, "downloadLocation": "NOASSERTION", "filesAnalyzed": False, "licenseConcluded": "NOASSERTION", "licenseDeclared": "MIT" if module["Path"] in ("github.com/universal-api/platform", "github.com/lib/pq") else "NOASSERTION", "copyrightText": "NOASSERTION"}
    if module.get("Version"):
        package["versionInfo"] = module["Version"]
        package["externalRefs"] = [{"referenceCategory": "PACKAGE-MANAGER", "referenceType": "purl", "referenceLocator": "pkg:golang/" + module["Path"] + "@" + module["Version"]}]
    if module.get("Main"):
        package["comment"] = "Source candidate; MIT intent requires contributor/relicensing rights signoff. This document does not attest a compiled binary."
        relationships.append({"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": ident})
    else:
        relationships.append({"spdxElementId": "SPDXRef-Package0", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": ident})
    packages.append(package)
print(json.dumps({"spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT", "name": "UWA source dependencies", "documentNamespace": "https://spdx.org/spdxdocs/uwa-source-" + identity, "creationInfo": {"creators": ["Tool: UWA source SBOM generator"], "created": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")}, "packages": packages, "relationships": relationships}, indent=2))
