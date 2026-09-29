# Vyra Health's fork of maestro-runner

A fork of [devicelab-dev/maestro-runner](https://github.com/devicelab-dev/maestro-runner) with
patches for running Maestro flows on a **physical iPhone plugged into another machine**, and for
running them the way Maestro itself does. The Mac running Xcode controls the phone over the
network, and the phone's WebDriverAgent port is forwarded through the machine the phone is
plugged into.

## Branches and tags

- **`main`** is a copy of upstream `main`, brought up to date when a new build starts (§ A new
  build). Nothing is committed to it and nothing here reads it. A daily workflow used to sync it;
  vyra.6 dropped it, because its schedule never once ran in this fork and a build fetches upstream
  itself.
- **`v<base>-vyra.<n>`** tags are the builds we install. Each is an upstream base plus our
  patches, one commit each, so a tag's history is the patch series. The base is a release tag
  when the fixes we need are released, and an upstream `main` commit when they are not;
  `<base>` names it the way `git describe --tags` does. `v1.1.27-vyra.1` sits on the `v1.1.27`
  release, and `v1.1.27-57-gc985a19-vyra.6` (the current build) on upstream `main` at
  `c985a19`, 57 commits later and not yet released. Tags are never moved.
- **`vyra`** (the default branch) holds the current build's tree. The Vyra Health org keeps
  every default branch linear and changed only through pull requests, so `vyra` is never
  rebased: it moves forward one pull request per build, whose one commit carries that build's
  tree. Read the patch series from the build's tag, not from `vyra`'s log.

## The patches

A patch that fixes a plain bug, or a difference from Maestro that nobody could want, is on by
default. One that changes how a run behaves is behind a switch: an environment variable, read
when it is used, that is on when it is set to anything.

### For a physical iPhone

| Commit | Switch | What it does |
|---|---|---|
| `feat(wda): use an external forward for a physical device's WDA port` | `MAESTRO_WDA_EXTERNAL_FORWARD=1` | Skips the go-ios port forward and the "port already bound, device in use" checks, for a device whose WDA port something else already forwards to `127.0.0.1`. |
| `fix(ios): read a physical device's info through devicectl when go-ios cannot see it` | none | A device reached over the network is not on this machine's usbmuxd, so the run stopped at "get device info". Falls back to `xcrun devicectl device info details`. |
| `feat(wda): opt-in skip for clearKeychain on real iOS devices` | `MAESTRO_IOS_DEVICE_SKIP_CLEAR_KEYCHAIN=1` | Makes `clearKeychain` pass with a note on a real device, where it cannot work, so a suite shared with simulators runs unchanged. |
| `feat(wda): opt-in launch environment for every app launch` | `MAESTRO_WDA_LAUNCH_ENV='{"NAME":"value"}'` | Adds these variables to every launch of the app: the session's first launch and every `launchApp` after it (a flow's own `environment` wins). A Simulator can take such a variable from its launchd; a real device has no such place. Local StoreKit testing on a real iPhone needs the developer disk image's framework paths this way. |
| `feat(wda): opt-in app whose screen is read while it is in front` | `MAESTRO_WDA_DEFAULT_ACTIVE_APP=<bundle id>` | Sets WebDriverAgent's `defaultActiveApplication`: while that app is in the foreground, lookups read its screen instead of the app under test's. The StoreKit payment sheet on iOS 27 is `com.apple.ServicesPaymentAngel`, a process of its own. Maestro reads whichever app is in the foreground. |
| `feat(wda): opt-in auto-tap of permission prompts only` | `MAESTRO_WDA_PERMISSION_ALERTS_ONLY=1` | Drives WDA's alert monitor with `autoClickAlertSelector`: it taps a permission prompt's Allow or OK (or Don't Allow when denying) and nothing else. Without it WDA taps a matching button of any alert, in-app ones included, or the alert's last button. Maestro never taps an in-app alert. |
| `feat(wda): opt-in attach to a WDA that is already running` | `MAESTRO_WDA_ATTACH=1` | Uses a WebDriverAgent something else started (go-ios's `runwda` on the machine the phone is plugged into, for one) and that `127.0.0.1:<port>` reaches: no WDA is built, started or stopped, and the run waits up to 30 s for its `/status`. The runner otherwise starts WDA only through `xcodebuild`, so without Xcode on the host this is the way to a physical iPhone. |
| `fix(wda): clear a real device's app state without Xcode` | none | A `clearState` on a real device uninstalled and reinstalled the app through `xcrun devicectl`, which only a Mac has. Without devicectl it now uses go-ios's installation proxy and zip conduit over usbmuxd, neither of which needs the iOS 17+ tunnel. A Mac with Xcode is unchanged. |

### Maestro's behaviour, on by default

| Commit | What it does |
|---|---|
| `fix(runscript): run a script file as written` | Script files were `${...}`-expanded as a whole before running, so template literals using the script's own variables became `"undefined"`. |
| `fix(wda): read the focused field back after typing into it` | Upstream reads the field back only for `inputText` with a selector; typing into the focused field is how a flow usually types. |
| `fix(wda): launchApp stops a running app first unless stopApp is false` | Maestro stops the app before launching it by default; the WDA driver only activated a running app, so a relaunch did not restart it. |
| `fix(wda): send a read or a lookup again when its connection drops` | WebDriverAgent drops connections late in a long session; a read, or a lookup that changes nothing, is sent once more. |
| `fix(wda): keep enough connections open for four requests at once` | The driver sends four requests at once, and Go kept two idle connections, so each burst opened two new ones. Through a phone's forward, new connections opened together failed in pairs with EOF; kept ones never did. |
| `fix(executor): retry's maxRetries counts retries, not attempts` | Maestro runs a `retry` block maxRetries + 1 times (1 by default, at most 3). The runner ran it maxRetries times (3 by default, no cap), so `maxRetries: 1` never retried. |
| `fix(executor): run onFlowComplete before the result, and fail on it` | As in Maestro, a failing `onFlowComplete` step fails a flow that had passed, and the hook runs before the flow is reported. |
| `fix(executor): remove the env keys a runFlow or retry added when it ends` | Keys a subflow added are removed when it returns, not set to "". |
| `fix(executor): read when: true: and assertTrue values as Maestro does` | Only blank, "false", "undefined", "null" and 0 are false. An unset `${...}` no longer runs the branch Maestro skips. |
| `fix(jsengine): give a script's http call Maestro's 5 minutes` | The script http timeout was 30 s. |
| `fix(executor): the pre-session scan finds a launchApp in retry and repeat` | The session's alert handling comes from the flow's first `launchApp`; one inside `retry`, `repeat` or a `runFlow` else branch was missed, and nested files resolved against the wrong directory. |
| `fix(wda): scrollUntilVisible honours centerElement` | A found element must have its center in the leading part of the screen, for up to 5 more looks, as in Maestro. |
| `fix(wda): a swipe's start and end without % are points, as in Maestro` | `start: "100, 500"` was read as 100% and 500% of the screen. |
| `fix(executor): tap options cost nothing on iOS unless the retry is on` | `retryTapIfNoChange: false` or a bare `waitToSettleTimeoutMs` read the page source four times around every tap (about 8 s on a phone). `true` follows Maestro's screenshot-based retry. |
| `fix(wda): a long press holds 3 s, as Maestro's does on iOS` | It held 1 s. |
| `fix(wda): checked selectors work on iOS` | `checked` was dropped with a warning and matched any state. Now a Switch, CheckBox or Toggle with value "1" is checked, as in Maestro. |
| `fix(wda): an element is not visible only when a lookup finds it absent` | A failed page source or rect read counted as "not visible", so `assertNotVisible` passed on errors. |
| `fix(wda): copyTextFrom never copies an empty string for a failed read` | A failed text read copied "". It reads again, then takes value, placeholder or label from the page source, as Maestro does, or fails. |
| `fix(wda): the crash-loop latch ends with its flow, and ignores transport errors` | Four quick "app died" errors failed every later step of every later flow in the run, and dropped connections counted as the app dying. |
| `fix(wda): a lookup looks at the screen once, whatever time is left` | Maestro always makes one attempt; the lookup checked its deadline first, so a condition with no budget left could be judged without a look. |
| `fix(wda): scrollUntilVisible reads one page source a pass` | Each pass read the page source twice, for the lookup and for the end-of-content check; on a real phone that is 2 to 3 s each. |
| `refactor(wda): name the swipe clamp after Maestro's coerceIn` | A rename. |

### Maestro's behaviour, behind a switch

| Switch | Commits | What it does |
|---|---|---|
| `MAESTRO_NO_SUITE_EXPANSION=1` | `feat(executor): opt-in switch to run a runFlow-only flow as one flow` | maestro-runner runs a flow made only of `runFlow` steps as a suite, one flow per step, with each step's `env` unexpanded. Maestro runs it as one flow. |
| `MAESTRO_STRICT_TYPING=1` | `feat(core): opt-in retype when a field holds more than was typed`, `feat(core): strict typing reads a field back once it holds still`, `fix(core): strict typing waits long enough for the keyboard's late character`, `fix(core): strict typing does not wait on a field it cannot read` | The typing read-back waits for the field to hold still (reads 1.2 s apart, at most 3 s), then retypes once a field that holds more than was typed. A real iPhone's keyboard sometimes adds a character, such as a second `@` about a second after an email. A field that cannot be read back, such as a code box that hands focus on, is not waited on. |
| `MAESTRO_WDA_TIMED_SWIPE=1` | `feat(wda): opt-in timed swipes, so a short swipe is a fling`, four `feat(wda): with timed swipes, …` commits, `feat(wda): a timed swipe moves in 100 ms and rests for its duration, as Maestro's does` | Swipe, scroll and scrollUntilVisible become Maestro's pointer gesture: the finger moves to the end in 100 ms and rests there for the duration before it lifts (Maestro's EventRecord). Durations and points are Maestro's: 400 ms by default, from an element's center to the screen's edge, `scroll` from the middle in 333 ms, scrollUntilVisible 40% of the screen. Without it, a swipe's `duration` was the hold before a drag XCUITest paced itself. |
| `MAESTRO_PARITY_TIMEOUTS=1` | `feat(executor): opt-in Maestro budget for when: and while: checks`, `feat(wda): opt-in Maestro lookup timeouts` | Lookups wait Maestro's 17 s (7 s when optional); `assertNotVisible` 17 s instead of 5. A `when:` or `while:` visibility check gets 7 s minus the time since the last step that changed the device, instead of a flat 1 s. An explicit timeout still wins. |
| `MAESTRO_STRICT_SELECTORS=1` | `feat(wda): opt-in strict text selectors, …`, `… strict id selectors, …`, `… strict tapOn, …`, `… strict visibility, …`, `… strict relative selectors and ties, …`, `… strict size selectors, …` | Selectors mean what they mean in Maestro. A text is a case-insensitive regex that must match the whole of the text, value or label, not a substring (`'Resend code'` no longer matches "Resend code in 0:42"). An id matches the whole identifier. A key-named `tapOn` never presses the key. An element counts as on screen when 10% of it is. `below:` and the rest compare with any anchor, the deepest match in tree order wins, and the element itself is tapped. A size has no tolerance unless one is given. |
| `MAESTRO_WDA_SETTLE=1` | `feat(wda): opt-in screen settle, the way Maestro waits on iOS` | Before and after the steps Maestro waits around, waits until two screenshots in a row are identical (at most 3 s), and after a swipe or scroll re-reads an element's position until it holds still before tapping it. |
| `MAESTRO_WDA_COORDINATE_TAP=1` | `feat(wda): opt-in element taps as Maestro's 100 ms touch` | An element tap is a 100 ms touch at the center of the element's bounds, as in Maestro, instead of XCUITest's element tap, which can scroll the view first. |

The devicectl fallback, the script-file fix, the focused-field read-back, the launchApp stop and
the parity fixes above are good candidates to offer upstream.

## Testing

`go test ./...` is **not** only unit tests. `pkg/device` drives the first device `adb devices`
lists (it removes every adb forward on it and installs and uninstalls Appium's settings and
UIAutomator2 APKs), `pkg/simulator` creates and deletes a simulator, `pkg/emulator` can stop an
emulator, and `pkg/cli`'s `TestCheckVisibleDevicesNeverErrors` asks usbmuxd for devices. On a
machine with devices attached, run the packages you touched, or the rest behind stubs:

```bash
mkdir -p /tmp/devshim
for t in adb xcrun xcodebuild devicectl simctl ios idevice_id ideviceinfo iproxy emulator avdmanager sdkmanager; do
  printf '#!/bin/sh\nexit 1\n' >"/tmp/devshim/$t"; chmod +x "/tmp/devshim/$t"
done
go list ./... | grep -v -E '/pkg/(device|simulator|emulator)$' \
  | PATH="/tmp/devshim:$PATH" xargs go test -skip TestCheckVisibleDevicesNeverErrors
```

`pkg/driver/mock`'s `TestExecute_SuccessTapStep` fails now and then on upstream as well: it
expects a mock tap to take measurable time.

## Building

From a checkout of a `v*-vyra.*` tag (Go 1.25 or later), so the binary names its commit:

```bash
M=github.com/devicelab-dev/maestro-runner/pkg/cli
V=1.1.27-57-gc985a19+vyra.6   # the tag without its leading v, with + before "vyra"
go build -trimpath -o bin/maestro-runner \
  -ldflags "-s -w -X $M.Version=$V -X $M.Commit=$(git rev-parse --short HEAD) -X $M.BuildDate=$(date -u +%F)" .
```

The binary looks for its drivers in `<home>/drivers`, where `<home>` is the directory above
`bin/`, or `$MAESTRO_RUNNER_HOME`. So either run it from the checkout, or install `bin/` and
`drivers/` side by side.

## A new build

**When.** A build moves to a new upstream base when upstream releases, not on every commit to
upstream `main`. Vyra Health's meta repo has a weekly watch on the night's pin
(`bin/vyra-tool-pin-check`, tool `maestro-runner`): it files a notice the first Monday a release
lands past our base, and goes red once the pin has been behind for more than 60 days or three
minor lines. Rebase onto the release tag. An upstream `main` commit is a base only when a fix we
need is not released yet, as it was for vyra.1 to vyra.6.

A new upstream base, a new patch, or both, start from the current build's tag:

```bash
git fetch upstream --tags && git fetch origin --tags
gh repo sync vyrahealth/maestro-runner --branch main    # main catches up with upstream
git switch -c series/v1.1.28 v1.1.27-57-gc985a19-vyra.6
git rebase --onto v1.1.28 c985a19     # c985a19: the base the series sits on now
# add or drop patches here, one commit each
# test it as Testing, above, says: not a bare go test ./...
git tag -a v1.1.28-vyra.1 -m "..." && git push origin v1.1.28-vyra.1
```

Leave out every patch upstream now has (§ Proposed upstream). After the rebase, `git cherry -v
v1.1.28 HEAD` marks with `-` a patch whose change upstream already carries; one that upstream
took in another form needs reading, and usually conflicts.

Then `vyra` takes the new build's tree in one commit, through a pull request:

```bash
C=$(git commit-tree "v1.1.28-vyra.1^{tree}" -p origin/vyra -m "Build v1.1.28-vyra.1")
git push origin "${C}:refs/heads/build/v1.1.28-vyra.1"   # braces: zsh reads "$C:r" as a modifier
gh pr create --base vyra --head build/v1.1.28-vyra.1 --title "Build v1.1.28-vyra.1" --body "..."
```

Merge it with "Squash and merge", the only method the org allows. The commit's parent is `vyra`
itself, so the squash is that one commit again: the history stays linear and nothing is
rewritten.

## Proposed upstream

A plain bug fix goes upstream once it has been proved here, so the series shrinks. Each proposal
is one patch on upstream `main`, without this file, with a CHANGELOG line. The patches behind a
switch stay here, and so do the ones for a phone reached over the network.

| Patch | Upstream pull request |
|---|---|
| `fix(wda): keep enough connections open for four requests at once` | [#181](https://github.com/devicelab-dev/maestro-runner/pull/181) |
| `fix(executor): retry's maxRetries counts retries, not attempts` | [#182](https://github.com/devicelab-dev/maestro-runner/pull/182) |
| `fix(wda): launchApp stops a running app first unless stopApp is false` | [#183](https://github.com/devicelab-dev/maestro-runner/pull/183) |
| `fix(wda): an element is not visible only when a lookup finds it absent` | [#184](https://github.com/devicelab-dev/maestro-runner/pull/184) |
| `fix(wda): checked selectors work on iOS` | [#185](https://github.com/devicelab-dev/maestro-runner/pull/185) |

The other plain fixes in § Maestro's behaviour, on by default follow once these have been
reviewed, so upstream sees a few at a time. Four of them do not stand alone on upstream `main`
yet: copyTextFrom and swipe points without `%` conflict there, one page source a scroll pass
builds on the centerElement fix, and the truthiness fix's test uses a helper from another patch.
