# ADR 0001 — Where the writ bridge lives

**Status:** accepted 2026-08-15; implemented 2026-08-15
**Affects:** mgtt (`mgtt verify`, `mgtt model export`), and
[writ-lang/writ](https://github.com/writ-lang/writ) (`writ mgtt`)

## Question

`mgtt verify` checks a model by handing it to writ, an external tool. Something
has to know both mgtt's export schema and writ's syntax. Where does that
knowledge live, and should either project carry a command that depends on the
other?

## What we built first, and what was wrong with it

The translation went into writ's tree as a `writ mgtt` verb, and mgtt grew a
`mgtt verify` command that shells out to it. Both projects ended up naming the
other.

Two things are wrong with that, and neither is about code size.

**mgtt shipped a verb that depends on a third-party tool.** `mgtt verify` is in
`mgtt --help` and does nothing without a binary mgtt does not control, does not
version, and cannot vendor. Every other mgtt command works from a clean
install. This one advertises a capability that may not be there.

**writ's tree carried a dialect for one specific product.** writ is a language.
SQL is a notation standard that does not move; mgtt is a product at v0.2.0 with
a schema that does. One such reading is defensible. The second one turns a
language into a collection of bridges wearing a language's name.

The original version of this document argued the second point and decided to
live with it, with "the second third-party dialect" as a trigger to revisit.
That was the wrong call: it accepted a structure we already knew was wrong and
deferred the fix to a future that might not notice.

## Decision

**Neither project owns the bridge. A separate tool does, and the three compose
on the command line.**

```
mgtt model export --json  |  mgtt2writ  |  writ check --stdin
```

- **mgtt** emits its resolved model as JSON. It mentions writ nowhere, in code
  or in `--help`. `mgtt verify` is removed.
- **writ** reads models. It mentions mgtt nowhere. `writ mgtt` is removed.
- **`mgtt2writ`** is the only thing that knows both. It reads the export on
  stdin and writes a writ model on stdout.

This is the pattern mgtt already uses for backends. A provider is an external
plugin precisely so core stays lean and knows nothing about kubectl or the AWS
CLI. The bridge is the same shape: an adapter between two systems, owned by
neither, living where an adapter belongs.

It is also the pattern writ already documents for SQL — `writ sql schema.sql >
shop.writ` then `writ check shop.writ`, two verbs and a pipe, with no wrapper
fusing them. We copied that precedent for the reading and ignored it for the
invocation.

## Consequences

**What each project keeps.** mgtt keeps `mgtt model export --json`, which is
writ-agnostic and equally useful to a visualiser or an editor. writ keeps
nothing mgtt-specific; it gains `--stdin` on all seven of its model-reading
verbs, which is a general convenience for any generated model and not a
concession to this bridge.

**One place to break.** `mgtt2writ` is the single thing that must track
both mgtt's schema and writ's syntax. That is a real maintenance surface with
no home team, and it is the honest cost of this decision. It is preferable to
the alternative, where the same knowledge is split across two projects that
each think the other owns it.

**It carries its own JSON.** The translation depends on writ's JSON reader
today (about 300 lines). Extracted, it vendors that rather than depending on
writ as a library, so the tool stays standalone.

**Discoverability drops.** `mgtt verify` was one word in `--help`. A pipeline
is three commands documented on a page. This is a real loss for the primary
persona — STRATEGY.md names authoring and CI ergonomics as a track, on the
reasoning that a model only stays honest if the loop is low-friction. The
pipeline must therefore be documented as a copy-pasteable CI block, not left as
an exercise.

**mgtt's documentation now tracks writ's verb surface, and its code does not.**
The decision requires mgtt's code and `--help` to name writ nowhere, and that
holds. But `verification.md` documents the pipeline — the decision demands it,
since discoverability is the acknowledged cost — and its advanced section names
six writ verbs. If writ renames one, an mgtt page goes stale. That is a far
cheaper failure than a broken build, and documentation is the right place for
the coupling to sit; it is recorded here so it is written down rather than
discovered.

**Nothing regresses.** `mgtt verify` is one day old and has never shipped in a
release. Removing it takes away a capability that arrived with the dependency;
there is no earlier behaviour to fall back to and nothing else in mgtt changes.

## Notes

**Naming.** Two names, and the distinction between them is the resolution of an
argument that ran through several rounds.

`mgtt2writ` is the **translator**: the Unix idiom (`dos2unix`, `pdf2ps`), short,
and it reads in the direction the data flows. A name ending in "check" was
rejected for it, because translation is its whole job and `writ check` does the
checking — such a name would put the pipeline's purpose on the one component
that does not serve it.

`mgtt-contradict-check` is the **wrapper**, shipped in the same repo, which runs
`mgtt model export --json | mgtt2writ | writ check --stdin` and exits with
writ's status. Here "-check" is true rather than a lie: this script does run the
check. That is the whole distinction — the name was wrong on the translator and
is right on the wrapper.

**Version skew.** Three components now version independently. The bridge tool
is where the constraint belongs: it refuses an export version it does not know
(already implemented) and should state which writ versions it emits for. The
pinned export fixture in its test suite catches mgtt changing a field's meaning
without bumping the version.

## Rejected

- **Keep both wrappers** — the status quo this replaces.
- **mgtt generates writ syntax in Go.** writ's language moves faster than
  mgtt's schema, so mgtt would maintain the fragile half from the side least
  able to see a breaking change coming.
- **Vendor the writ binary into mgtt.** Solves the missing-dependency problem
  without addressing the structural one, and pins mgtt to a writ version.
