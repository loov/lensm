# asmref

A generated, flattened instruction reference for hover tooltips. `table.json.gz`
(gzip-compressed JSON) is embedded at build time and looked up by mnemonic via
`asmref.Lookup`.

The table holds the factual "API" — brief title, description, syntax forms and
per-operand meanings — plus x86 per-microarchitecture performance data (uops,
ports, latency, throughput). Bit encodings and execution pseudocode are dropped.
The Go-pseudocode effects shown in tooltips come from `internal/asmhelp`, not
from here; `asmref` only fills in reference text `asmhelp` doesn't already cover.

The full table (all mnemonics, all measured microarchitectures) is ~15MB of JSON
that gzips to ~0.6MB, hence the compressed embed.

## Regenerating

```
go generate ./internal/asmref
```

With no arguments the generator reads the small checked-in fixtures under
`gen/testdata`, so a bare run stays reproducible. To build the full table,
download the sources (`data/download.sh`) and point it at them:

```
go run ./internal/asmref/gen \
  -arm  data/arm64/ISA_A64_xml_A_profile-2025-12 \
  -x86  data/x86/instructions.xml \
  -out  internal/asmref/table.json.gz
```

The generator also prints (to stderr) any mnemonics that `golang.org/x/arch`'s
decoders know but the table is missing — a coverage hint, not an error.

## Sources

- **ARM AArch64** — the official "Exploration tools / ISA XML" release from
  developer.arm.com (the `ISA_A64_xml_*` bundle, one XML file per instruction).
  Files starting with `sysreg_` (system registers) and `AArch64-` (shared
  pseudocode) are skipped.
- **x86 / AMD64** — `instructions.xml` from https://uops.info (XED-derived,
  ~140MB). It is a benchmark dataset, not a manual: there are no description or
  syntax elements, so the `summary` attribute becomes the brief and the `string`
  attribute (e.g. `ADD (R32, M32)`) becomes a syntax form. Per-operand
  descriptions are not emitted — they restate the token already in the form.
  For every operand form and every measured microarchitecture, the first
  `<measurement>` yields uops, ports (uops.info notation, e.g. `1*p0156`),
  throughput and worst-case latency; `<IACA>` estimate nodes are skipped in
  favour of the real measurements.

`data/download.sh` fetches both dumps (URLs/version pinned to match
`golang.org/x/arch/arm64/instgen/xmlspec`). They are large and gitignored;
record the release version you used so regeneration stays reproducible.
