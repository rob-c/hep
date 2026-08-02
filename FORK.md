About this fork
===============

Upstream go-hep has moved to Codeberg, which has demonstrably worse uptime than
GitHub and an aggressive, somewhat euro-centric set of anti-LLM policies. This
fork ignores both, for the sake of ***GETTING THE JOB DONE***. Work lands here
when it works and is tested; we do not kick the can down the road for the sake
of posturing.

What this fork adds is summarised at the top of the [README](README.md).

## Where the work is

**All work goes on in `main`**, the default branch — clone it and you have it.
There are two branches and no others:

| branch     | what it is                                                                    |
|------------|-------------------------------------------------------------------------------|
| `main`     | this fork's work, rebased onto upstream as upstream moves                      |
| `upstream` | an exact mirror of [Codeberg](https://codeberg.org/go-hep/hep)'s `main`, refreshed daily and never written to by hand |

Keeping the mirror as a branch of this repository is what makes `main` legible:
`git diff upstream main` is exactly the fork, and nothing else.

## How `upstream` stays current

GitHub's *Sync fork* button is of no use here: it can only pull from the
archived `go-hep/hep` parent on GitHub, which will never move again, and it has
no way to reach Codeberg. So the mirror is a push of our own —
[`.github/workflows/mirror-upstream.yml`](.github/workflows/mirror-upstream.yml)
fetches Codeberg every morning and force-updates `upstream` to match it.

The job runs from `main` rather than from the branch it writes, so refreshing
the mirror cannot delete the job doing it. To refresh out of band:

```
gh workflow run "Mirror upstream" -R rob-c/hep
```

or, from a clone with Codeberg configured as the `upstream` remote:

```
git fetch upstream
git push --force fork upstream/main:upstream
```

## Rebasing onto a newer upstream

```
git fetch upstream
git rebase upstream/main
```

Conflicts are the normal kind: upstream sweeps (`go fix`, `testing.TB.TempDir`,
dependency bumps) landing on files this fork has also changed. Resolve them in
favour of upstream's idiom applied to our code, then `go build ./...`,
`go vet ./...` and `go test ./...` before force-pushing `main`.
