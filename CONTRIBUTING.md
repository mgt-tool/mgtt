# Contributing to mgtt

Bug reports, questions and patches are all welcome. Start with an issue if the
change is larger than a fix — it is cheaper to agree on the shape before the
code exists.

## Before your first pull request: sign the CLA

Every contribution is accepted under the Contributor License Agreement in
[CLA.md](CLA.md). Open a pull request and a bot comments with a link; signing
is one click, and it covers everything you contribute here from then on.

**What it does.** You keep the copyright in everything you write. You grant the
project's owner a licence broad enough to ship your patch under the project's
current licence and to change that licence later. That is what keeps one person
able to answer "may I use this, and how?" for the whole codebase — without it,
a licence change would need every contributor who ever sent a patch to agree.

**What it does not do.** It is not a copyright assignment. It gives nobody
rights to your other work. It does not stop you using your own contribution
anywhere else, for anything, including commercially.

If your employer owns the copyright in what you write at work, you need their
sign-off — section 4 covers this.

## Sending a patch

- Work on a branch; one concern per pull request.
- Run the repository's tests before you open it. The README says how.
- New source files need the two-line header every other file carries — a
  copyright line and an `SPDX-License-Identifier`. Copy them from a neighbour.
- Write the commit message for someone reading it in a year with no memory of
  the discussion: what changed, and why it had to.

## Releasing

A release is a git tag `vX.Y.Z`; everything else follows from it in CI
(`.github/workflows/release.yaml`). Before tagging, the tree has to agree with
itself about the version, and `make tag` refuses until it does:

1. Set `VERSION` to `X.Y.Z` (a breaking change under `[Unreleased]` means the
   next minor while we are on 0.x).
2. In `CHANGELOG.md`, retitle `## [Unreleased]` to `## [X.Y.Z] — YYYY-MM-DD`
   and start a fresh, empty `## [Unreleased]` above it.
3. Commit, then `make tag`.

`scripts/release-check.sh` is the rule, in one place: CI runs it on every
push in its everyday form (VERSION is a version the changelog knows), and the
release workflow runs it at the tag in its strict form (the tag spells
VERSION, the section is dated, `[Unreleased]` is empty). A release that ships
work the changelog still calls unreleased is refused.

What a tag produces, and what the workflow then checks from the outside as a
client would: a GitHub release with `mgtt-<os>-<arch>` for four platforms and
`SHA256SUMS`, and notes quoted from the changelog section; the image at
`ghcr.io/mgt-tool/mgtt:X.Y.Z` (plus `X.Y`, `X`, `latest`) for linux/amd64 and
linux/arm64; and the module at `vX.Y.Z` on the Go proxy, for `go install` and
for `go get …/sdk/provider`.

