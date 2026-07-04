# asmref

A generated, flattened instruction reference for hover tooltips. `table.json` is
embedded at build time and looked up by mnemonic via `asmref.Lookup`.

The table holds only the factual "API" — brief title, description, syntax forms
and per-operand meanings. Bit encodings, latency/port data and execution
pseudocode are intentionally dropped. The Go-pseudocode effects shown in
tooltips come from `internal/asmhelp`, not from here; `asmref` only fills in
reference text for mnemonics `asmhelp` doesn't already cover.

## Regenerating

```
go generate ./internal/asmref
```

With no arguments the generator reads the small checked-in fixtures under
`gen/testdata`, so a bare run stays reproducible. To build the full table, point
it at real source dumps:

```
go run ./internal/asmref/gen \
  -arm  /path/to/ISA_A64_xml \
  -x86  /path/to/uops.info/instructions.xml \
  -out  internal/asmref/table.json
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
  attribute (e.g. `ADD (R32, M32)`) becomes the syntax. Per-operand descriptions
  are not emitted — they are derivable from the token already in the syntax and
  would otherwise duplicate ~69 generic strings tens of thousands of times.
  `<architecture>`/`<measurement>` micro-op tables are skipped.

These dumps are large and not redistributed here; download them into a local
directory and pass the paths above. Record the exact release version you used
when you regenerate so the output is reproducible.
