#!/usr/bin/env bash
#
# Downloads the CPU ISA source dumps that internal/asmref/gen parses into
# table.json. Both files are large and gitignored (see .gitignore); this script
# fetches them into .data/ so the generator can be pointed at them.
#
# Usage:
#   .data/download.sh          # download anything missing
#   FORCE=1 .data/download.sh   # re-download even if present
#
# After it finishes it prints the exact `go run ./internal/asmref/gen ...`
# command with the resolved paths.
set -euo pipefail

DATA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FORCE="${FORCE:-0}"

# x86: uops.info (XED-derived) instruction + port data. ~140MB.
X86_URL="https://uops.info/instructions.xml"
X86_FILE="$DATA_DIR/x86/instructions.xml"

# arm64: ARM's official A64 ISA XML. URL and version are pinned to match the
# release golang.org/x/arch/arm64/instgen/xmlspec expects (its ExpectedURL).
ARM_VERSION="ISA_A64_xml_A_profile-2025-12"
ARM_URL="https://developer.arm.com/-/cdn-downloads/permalink/Exploration-Tools-A64-ISA/ISA_A64/${ARM_VERSION}.tar.gz"
ARM_TARBALL="$DATA_DIR/arm64/${ARM_VERSION}.tar.gz"
ARM_DIR="$DATA_DIR/arm64/${ARM_VERSION}"

fetch() { # url dest
	echo "  downloading $1"
	curl -fL --progress-bar -o "$2" "$1"
}

echo "==> x86 (uops.info)"
if [[ "$FORCE" != "1" && -s "$X86_FILE" ]]; then
	echo "  present, skipping: $X86_FILE"
else
	mkdir -p "$(dirname "$X86_FILE")"
	fetch "$X86_URL" "$X86_FILE"
fi

echo "==> arm64 (ARM A64 ISA XML, ${ARM_VERSION})"
if [[ "$FORCE" != "1" && -d "$ARM_DIR" ]]; then
	echo "  present, skipping: $ARM_DIR"
else
	mkdir -p "$DATA_DIR/arm64"
	fetch "$ARM_URL" "$ARM_TARBALL"
	echo "  extracting"
	tar xzf "$ARM_TARBALL" -C "$DATA_DIR/arm64"
	rm -f "$ARM_TARBALL"
	xml_count=$(find "$ARM_DIR" -name '*.xml' | wc -l | tr -d ' ')
	echo "  extracted ${xml_count} xml files"
fi

echo
echo "Sources ready. Generate the real table with:"
echo
echo "  go run ./internal/asmref/gen \\"
echo "    -arm     $ARM_DIR \\"
echo "    -x86     $X86_FILE \\"
echo "    -x86arch ADL-P \\"
echo "    -out     internal/asmref/table.json"
