# Vyra Health's fork of maestro-runner

A fork of [devicelab-dev/maestro-runner](https://github.com/devicelab-dev/maestro-runner) with a
few patches for running Maestro flows on a **physical iPhone plugged into another machine**. The
Mac running Xcode controls the phone over the network, and the phone's WebDriverAgent port is
forwarded through the machine the phone is plugged into.

## Branches and tags

- **`main`** mirrors upstream `main`. `.github/workflows/sync-upstream.yml` fast-forwards it every
  day. Nothing is committed to it directly.
- **`vyra`** (the default branch) is one upstream release tag plus our patches, one commit each.
  Today that is `v1.1.27` plus the four commits below.
- **`v<upstream>-vyra.<n>`** tags mark the builds we install, for example `v1.1.27-vyra.1`.

## The patches

| Commit | Switch | What it does |
|---|---|---|
| `feat(wda): use an external forward for a physical device's WDA port` | `MAESTRO_WDA_EXTERNAL_FORWARD=1` | Skips the go-ios port forward and the "port already bound, device in use" checks, for a device whose WDA port something else already forwards to `127.0.0.1`. |
| `fix(ios): read a physical device's info through devicectl when go-ios cannot see it` | none | A device reached over the network is not on this machine's usbmuxd, so the run stopped at "get device info". Falls back to `xcrun devicectl device info details`. |
| `fix(runscript): run a script file as written` | none | Script files were `${...}`-expanded as a whole before running, so template literals using the script's own variables became `"undefined"`. Has a regression test. |
| `feat(wda): opt-in skip for clearKeychain on real iOS devices` | `MAESTRO_IOS_DEVICE_SKIP_CLEAR_KEYCHAIN=1` | Makes `clearKeychain` pass with a note on a real device, where it cannot work, so a suite shared with simulators runs unchanged. |

The second and third are general bug fixes, and good candidates to offer upstream.

## Building

From a checkout of a `v*-vyra.*` tag (Go 1.25 or later):

```bash
M=github.com/devicelab-dev/maestro-runner/pkg/cli
go build -trimpath -ldflags "-s -w -X $M.Version=1.1.27+vyra.1 -X $M.Commit=$(git rev-parse --short HEAD)" -o bin/maestro-runner .
```

The binary looks for its drivers in `<home>/drivers`, where `<home>` is the directory above
`bin/`, or `$MAESTRO_RUNNER_HOME`. So either run it from the checkout, or install `bin/` and
`drivers/` side by side.

## Moving to a new upstream release

```bash
git fetch upstream --tags
git rebase --onto v1.1.28 v1.1.27 vyra   # resolve anything, then: go test ./...
git tag v1.1.28-vyra.1
git push --force-with-lease origin vyra && git push origin v1.1.28-vyra.1
```

Rebasing rewrites `vyra`, so it needs a force push. Every build we installed stays reachable
through its tag.
