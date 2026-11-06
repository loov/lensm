# Agent notes

## Commit messages

Follow the Go project style.

```
graph: draw merge edges with the parent's colour

The edge took the child's colour, so two branches merging into one
looked like a single line. gitk colours by parent, do the same.

Fixes #12
```

- First line is `area: what changed`. The area is the file or part of the
  app the change touches (`graph`, `diff`, `find`, `ui`, `core`). Use
  `all:` for repo-wide changes.
- The summary after the colon is lowercase and has no trailing period. It
  completes the sentence "This change modifies gr to ...". Keep the whole
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
