About this fork
===============

Upstream go-hep has moved to Codeberg, which has demonstrably worse uptime than
GitHub and an aggressive, somewhat euro-centric set of anti-LLM policies. This
fork ignores both, for the sake of ***GETTING THE JOB DONE***. Work lands here
when it works and is tested; we do not kick the can down the road for the sake
of posturing.

## Where the work is

**All work goes on in the
[`xrootd-client-parity`](https://github.com/rob-c/hep/tree/xrootd-client-parity)
branch.** It is the default branch of this repository, so it is what you get by
cloning, and what the front page shows. Nothing is developed on `main`.

| branch                 | what it is                                                      |
|------------------------|-----------------------------------------------------------------|
| `xrootd-client-parity` | this fork's work, rebased onto upstream as upstream moves        |
| `main`                 | an exact mirror of [Codeberg](https://codeberg.org/go-hep/hep)'s `main`, never written to by hand |

## How `main` stays current

GitHub's *Sync fork* button is of no use here: it can only pull from the
archived `go-hep/hep` parent on GitHub, which will never move again, and it has
no way to reach Codeberg. So `main` is mirrored by a push of our own —
[`.github/workflows/mirror-upstream.yml`](.github/workflows/mirror-upstream.yml)
fetches Codeberg every morning and force-updates `main` to match it.

The job runs from the default branch rather than from `main`, so force-updating
`main` cannot delete the job doing it. To resync out of band:

```
gh workflow run "Mirror upstream" -R rob-c/hep
```

or, from a clone with Codeberg configured as `upstream`:

```
git fetch upstream
git push --force fork upstream/main:main
```
