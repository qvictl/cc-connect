# Blackbox tests

## Credential-free source smoke

```bash
make test-release-smoke
```

Needs Go and the Claude Code (`claude`) and Codex (`codex`) CLIs on PATH.
It builds the current source with `no_web,cc_blackbox_loopback`, uses a
private child environment and home, and talks to a scripted loopback model.
The bridge binds `127.0.0.1` before the fixed
`TEST_ONLY_BLACKBOX_BRIDGE_TOKEN` fixture is used. Ordinary production builds
keep their existing listener behavior and cryptographic randomness.

`CC_BLACKBOX_BINARY` is rejected before launch or fixture config creation.
An old binary can advertise an unused build tag without implementing it, so
metadata alone is not a safe listener guarantee. This suite validates a
test-instrumented source build, **not an ordinary release artifact**. Never
publish a binary built with `cc_blackbox_loopback` as a release artifact.

The run fails if a required CLI is absent (`CC_BLACKBOX_REQUIRE_AGENTS`,
default `claudecode,codex`). Credential-free smoke runs only the binary
backend. Explicit credential-free in-process requests fail before agent
construction, because that mode shares the parent's environment and home.
This reduces in-process coverage; it does not silently claim both backends.

The child and build subprocess environment copies only `PATH`, `SystemRoot`
and `WINDIR`. Home, agent configuration, XDG, Windows profile and temporary
directories are private. Fake Codex uses a separate fresh private directory
under the test cache root for its helper-binary compatibility, rather than
the usual login/config directory. Provider credentials, proxies, agent overrides,
loader options and other parent variables are not inherited. `NoCredentials`
cases do not discover or wire provider credentials. These measures are not
an OS or outbound-network sandbox: real CLIs can run tools, access other
files, use system credential facilities, or contact other services. Other
processes on the same host can reach loopback and know the fixture token.

## Explicit live-provider tests

Credentials in the environment alone never enable live-provider tests.
They skip unless the test command explicitly includes `-blackbox-live`.
Only use that flag for a separately authorized live run. The isolated
binary backend additionally needs an explicit `CC_BLACKBOX_<AGENT>_API_KEY`
provider; normal login state and generic inherited keys are not copied.
In-process live tests retain their existing credential behavior behind the
same explicit flag. Fake-model tests are still not cleared for execution
merely by this source change.

```bash
go test -tags blackbox -count=1 ./tests/blackbox/smoke/
# Separately authorized live run only:
go test -tags blackbox -count=1 ./tests/blackbox/p0/ -args -blackbox-live
```

Pure regression sources cover the environment allowlist, private paths,
provider-mode classification and default/tagged listener address. Run the
listener regressions in both ordinary and `cc_blackbox_loopback` builds
when execution is authorized. Source review is not a runtime test pass.

## Fake model

`Options{FakeModel: true}` points the agent at `tests/blackbox/fakemodel`, a
scripted stand-in for the model API. The real agent CLI and the real
cc-connect run; only the model is fake, so turns are deterministic and need
no key. One server speaks both wire formats:

| Agent | Wire format | Provider wiring |
| --- | --- | --- |
| `claudecode` | Anthropic Messages (`/v1/messages`) | `base_url` = fake |
| `codex` | OpenAI Responses (`/v1/responses`) | `base_url` = fake + `/v1`, `wire_api = "responses"`, private `codex_home` per test |

The script is the message text:

| Message | Fake model does |
| --- | --- |
| `FAKE:reply <text>` | answers `<text>` |
| `FAKE:slow <n> <word>` | streams `<word>` n times, 300 ms apart |
| `FAKE:tool Bash <command>` | calls the shell tool (`Bash` / Codex `exec_command`); after the tool result, answers `TOOL_DONE: <output>` (or `TOOL_ERROR: …`) |
| `FAKE:escalate <command>` | like `FAKE:tool Bash`, but asks to run outside the sandbox; this is what makes Codex request approval (use `AgentOptions: {"backend": "app_server", "mode": "suggest"}`) |
| `FAKE:recall` | answers `HISTORY: ` plus the text of earlier turns |
| anything else | answers `ECHO: <first line>` |

`env.FakeModel.TurnRequests()` returns what the CLI actually sent (user
text, tool results, image blocks), so tests can assert on the model side as
well.

## Backends

`CC_BLACKBOX_BACKEND` selects what runs cc-connect:

| Backend | What runs | What it covers |
| --- | --- | --- |
| `inprocess` (default) | `core.Engine` inside the test process | engine, commands, sessions, agent adapter |
| `binary` | the real `cc-connect` process, reached through its bridge WebSocket (`docs/bridge-protocol.md`) | everything above plus config loading, startup, the bridge transport and the agent subprocess as deployed |

The binary backend builds `./cmd/cc-connect` once with
`-tags no_web,cc_blackbox_loopback`. Each environment has its own private
home, data dir, config and free ports. Authentication is a fixed test-only
fixture. A failure prints the process log tail with that fixture redacted.
It never accepts a prebuilt binary. Credential-free callers must explicitly
select `Options{Backend: BackendBinary}` or `CC_BLACKBOX_BACKEND=binary`.

Tests that call `NewEnvWithSetup` or touch `env.Engine` need the in-process
engine and skip on the binary backend (`env.RequireInProcess()`).

## Capability profiles

cc-connect renders a turn differently depending on which optional platform
interfaces exist (buttons, cards, streaming preview). Real platforms fall
into a few profiles, so testing the profiles covers the platform-side
behaviour of core without one test per real platform.
`CC_BLACKBOX_PROFILE` (or `Options.Profile`) selects one:

| Profile | Capabilities | Similar real platforms |
| --- | --- | --- |
| `legacy` (default) | cards, buttons, images, files — the historical mock | — |
| `text` | text only | LINE |
| `buttons` | inline buttons | QQ Bot |
| `streaming` | preview start / update / delete | Slack, Matrix |
| `card` | cards with navigation, buttons | — |
| `full` | cards, buttons, streaming preview, images, files | Feishu, Telegram |

In-process, the profile picks a wrapper type that exposes only those
interfaces; on the binary backend it becomes the bridge `register`
capability list.

## Clicking buttons

`env.Click("perm:allow", timeout)` or
`env.Platform.InjectCardAction(user, chat, action)` simulate a button click
with the bridge contract: `perm:allow|deny|allow_all` answer a permission
prompt, `cmd:/x` runs a command, `nav:`/`act:` go through the card
navigation handler. `SentMessage.ButtonValues()` lists the callbacks
offered by a message, whether from inline buttons or card elements.
