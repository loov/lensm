# Agent notes

## Commit messages

Follow the Go project style.

```
goobj: link tail calls to their target

A JMP to another function's entry got no link, so clicking a tail
call in Go, C or Rust code did nothing. Treat a reference to a
function's start as a call whatever the instruction.

Fixes #12
```

- First line is `area: what changed`. The area is the package or part of
  the app the change touches (`goobj`, `codeview`, `gui`, `mcp`,
  `comments`, `source`, `asmref`, `ui` for the root package's window
  code). Use `all:` for repo-wide changes.
- The summary after the colon is lowercase and has no trailing period. It
  completes the sentence "This change modifies lensm to ...". Keep the whole
  line under about 70 characters.
- Leave a blank line, then the body. Wrap it at 72 columns.
- The body explains why: what was wrong, what the change does about it,
  anything a reviewer would otherwise have to dig for. The diff already
  shows how.
- Use full sentences. Plain text, no markdown headings, no bullet soup.
- Skip the body only when the summary says everything.
- Put references like `Fixes #12` or `Updates #12` at the end.
- No `Co-Authored-By` or other attribution trailers.

## Unslop commit messages

Before committing, reread the message and fix these:

- Puffery and AI words. "enhance", "robust", "seamless", "leverage",
  "crucial", "comprehensive", "streamline", "ensure", "delve". Say what
  happened: "add", "fix", "remove", "use".
- Selling the change. "greatly improves", "significantly faster". Give the
  number or drop the adverb.
- Restating the diff. "Update graph.rs and main.rs" or a list of every
  touched function. Say why instead.
- Filler. "This commit", "In order to", "It is worth noting that". Start
  with the point.
- Padding the list. Don't force three bullets when there is one reason.
  Don't add "Also improved readability" for changes nobody asked about.
- Punctuation tells. No em dashes, no bold, no emoji, straight quotes only.
  Avoid colons mid-sentence, the one after the area prefix is the exception.
- Passive voice. "the cache is invalidated" becomes "reload clears the
  cache".
- Hedging. "should probably help with" becomes what it does, or say plainly
  that it is untested.

Last check: if the sentence would fit unchanged in another project's log,
it says nothing about this one. Rewrite or cut it.

## Workflow

Commit each change as soon as it is implemented and builds, one commit
per logical change.

## Testing

Run `go test ./...`. The C, C++ and Rust tests in
`internal/goobj/native_test.go` build programs with `cc`, `c++` and
`rustc`. They skip when a compiler is missing, except in CI (`CI` set),
where a missing compiler fails them. `go test -short ./...` skips every
test that builds a program.

To check a UI change without opening the app, `TestRender` draws the
window offscreen into a PNG. It skips unless `-render` is set:

```
go test -run TestRender . -render /tmp/out.png \
    -render.path ./testdata/testprog -render.func main.dot
```

The other `-render.*` flags pick the theme, sidebar panel, columns,
window size, pointer position and popups; see `render_test.go`. It
starts from the default settings and never writes the real ones.
