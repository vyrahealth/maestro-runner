# Vyra Health's fork of maestro-runner

A fork of [devicelab-dev/maestro-runner](https://github.com/devicelab-dev/maestro-runner) with a
few patches for running Maestro flows on a **physical iPhone plugged into another machine**. The
Mac running Xcode controls the phone over the network, and the phone's WebDriverAgent port is
forwarded through the machine the phone is plugged into.

## Branches and tags

- **`main`** mirrors upstream `main`. `.github/workflows/sync-upstream.yml` fast-forwards it every
  day. Nothing is committed to it directly.
- **`v<base>-vyra.<n>`** tags are the builds we install. Each is an upstream base plus our
  patches, one commit each, so a tag's history is the patch series. The base is a release tag
  when the fixes we need are released, and an upstream `main` commit when they are not;
  `<base>` names it the way `git describe --tags` does. `v1.1.27-vyra.1` sits on the `v1.1.27`
  release, and `v1.1.27-57-gc985a19-vyra.2` (the current build) on upstream `main` at
  `c985a19`, 57 commits later and not yet released. Tags are never moved.
- **`vyra`** (the default branch) holds the current build's tree. The Vyra Health org keeps
  every default branch linear and changed only through pull requests, so `vyra` is never
  rebased: it moves forward one pull request per build, whose one commit carries that build's
  tree. Read the patch series from the build's tag, not from `vyra`'s log.

## The patches

| Commit | Switch | What it does |
|---|---|---|
| `feat(wda): use an external forward for a physical device's WDA port` | `MAESTRO_WDA_EXTERNAL_FORWARD=1` | Skips the go-ios port forward and the "port already bound, device in use" checks, for a device whose WDA port something else already forwards to `127.0.0.1`. |
| `fix(ios): read a physical device's info through devicectl when go-ios cannot see it` | none | A device reached over the network is not on this machine's usbmuxd, so the run stopped at "get device info". Falls back to `xcrun devicectl device info details`. |
| `fix(runscript): run a script file as written` | none | Script files were `${...}`-expanded as a whole before running, so template literals using the script's own variables became `"undefined"`. Has a regression test. |
| `feat(wda): opt-in skip for clearKeychain on real iOS devices` | `MAESTRO_IOS_DEVICE_SKIP_CLEAR_KEYCHAIN=1` | Makes `clearKeychain` pass with a note on a real device, where it cannot work, so a suite shared with simulators runs unchanged. |
| `feat(executor): opt-in switch to run a runFlow-only flow as one flow` | `MAESTRO_NO_SUITE_EXPANSION=1` | maestro-runner runs a flow made only of `runFlow` steps as a suite, one flow per step, with each step's `env` unexpanded. Maestro runs it as one flow. The switch keeps Maestro's reading. |
| `feat(core): opt-in retype when a field holds more than was typed` | `MAESTRO_STRICT_TYPING=1` | The typing read-back also retypes, once, a field that holds more than was typed. On a real iPhone the keyboard sometimes adds a character (a second `@` after an email). |
| `fix(wda): read the focused field back after typing into it` | none | Upstream reads the field back only for `inputText` with a selector. This does the same for typing into the focused field, which is how a flow usually types. |
| `fix(wda): launchApp stops a running app first unless stopApp is false` | none | Maestro stops the app before launching it by default. The WDA driver only activated a running app, so a relaunch did not restart it. |

The devicectl fallback, the script-file fix, the focused-field read-back and the launchApp stop
are general bug fixes, and good candidates to offer upstream.

## Building

From a checkout of a `v*-vyra.*` tag (Go 1.25 or later), so the binary names its commit:

```bash
M=github.com/devicelab-dev/maestro-runner/pkg/cli
V=1.1.27-57-gc985a19+vyra.2   # the tag without its leading v, with + before "vyra"
go build -trimpath -o bin/maestro-runner \
  -ldflags "-s -w -X $M.Version=$V -X $M.Commit=$(git rev-parse --short HEAD) -X $M.BuildDate=$(date -u +%F)" .
```

The binary looks for its drivers in `<home>/drivers`, where `<home>` is the directory above
`bin/`, or `$MAESTRO_RUNNER_HOME`. So either run it from the checkout, or install `bin/` and
`drivers/` side by side.

## A new build

A new upstream base, a new patch, or both, start from the current build's tag:

```bash
git fetch upstream --tags && git fetch origin --tags
git switch -c series/v1.1.28 v1.1.27-57-gc985a19-vyra.2
git rebase --onto v1.1.28 c985a19     # c985a19: the base the series sits on now
# add or drop patches here, one commit each
go test ./...
git tag -a v1.1.28-vyra.1 -m "..." && git push origin v1.1.28-vyra.1
```

When upstream has released a fix one of the patches carries, leave that patch out.

Then `vyra` takes the new build's tree in one commit, through a pull request:

```bash
C=$(git commit-tree "v1.1.28-vyra.1^{tree}" -p origin/vyra -m "Build v1.1.28-vyra.1")
git push origin "${C}:refs/heads/build/v1.1.28-vyra.1"   # braces: zsh reads "$C:r" as a modifier
gh pr create --base vyra --head build/v1.1.28-vyra.1 --title "Build v1.1.28-vyra.1" --body "..."
```

Merge it with "Squash and merge", the only method the org allows. The commit's parent is `vyra`
itself, so the squash is that one commit again: the history stays linear and nothing is
rewritten.
