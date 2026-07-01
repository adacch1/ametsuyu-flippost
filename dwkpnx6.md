# zflip5-modem-module - Work Plan

## TL;DR (For humans)
**What you'll get:** A rooted Z Flip 5 modem module with a safe local control service, iPhone Shortcut commands, Discord slash commands, and status checks for network, tethering, thermal state, battery, and recent SMS. It will prefer 5G and recover 5G when the device is safe, but it will not fight thermal protection.

**Why this approach:** The root part should stay small and local-only because it can change device radios and expose SMS. Android framework data belongs in a helper app because telephony, SMS, battery, and thermal APIs are more reliable there than shell scraping on OneUI.

**What it will NOT do:** It will not disable Samsung/Android thermal mitigation, kill thermal daemons, edit vendor thermal files, force 5G during unsafe thermal states, expose an unauthenticated public root API, forward SMS by default, bypass carrier provisioning, or modify baseband/IMEI/SIM behavior.

**Effort:** Large
**Risk:** High - rooted device control plus SMS privacy, carrier behavior, and thermal/radio safety.
**Decisions I made for you:** Use local-only root daemon; use private relay/VPN/tunnel for remote access; use iPhone Shortcuts HTTPS JSON; use Discord interactions with signature verification and ephemeral sensitive replies; use Kotlin/Java helper APK for Android framework APIs; make SMS pull-only/redacted/rate-limited; implement safe 5G preference/recovery, not unsafe thermal bypass; treat 44 C as warning threshold and Android thermal status as the hard safety gate.

Your next move: run `$omo:start-work .omo/plans/zflip5-modem-module.md` when ready. Full execution detail follows below.

---

> TL;DR (machine): Large/high-risk plan for Magisk module + Android helper + local daemon + iPhone/Discord control + safe thermal-aware 5G recovery.

## Scope
### Must have
- A new project directory for the module, with Magisk module packaging: `module.prop`, installer/update script, `service.sh`, uninstall cleanup, structured config, and explicit module ID/name/version.
- Boot-time root service launched from Magisk `late_start service`, not blocking `post-fs-data` except where unavoidable.
- Local authenticated HTTP API bound to `127.0.0.1` by default, with command allowlist, bearer token auth, rate limiting, structured JSON, and logs.
- Android helper APK/service for framework APIs: `TelephonyManager`, `SubscriptionManager`, `PowerManager`, `BatteryManager`, SMS provider access, and permissions/default SMS handler flow where required.
- Status commands:
  - `/status`: summary of SIM/carrier, cellular generation, 5G/NR display state if available, signal, IP/tethering state, thermal status, battery level/temp/charging, daemon health.
  - `/network`: detailed service state, data network type, allowed/preferred network type when readable, subscription IDs, APN/tethering facts that are safe to expose.
  - `/thermal`: Android thermal status, battery temperature, thermal zone readings discovered on-device, 44 C warning flag, cooldown/recovery state.
  - `/battery`: level, plugged state, health/status when available, temperature, discharge/charge trend if available.
  - `/sms recent`: recent SMS list, pull-only, owner-authenticated, redacted by default, rate-limited.
  - `/tether on|off|status`: documented command path chosen from on-device discovery.
  - `/prefer5g`: prefer/recover NR when safe; no unsafe forcing.
  - `/cooldown`: disable heavy control attempts and optionally turn off tether/5G preference until thermal status safe again.
  - `/reboot-service`: restart only module daemon/helper, not whole phone, unless user explicitly adds phone reboot later.
- iPhone Shortcuts examples using HTTPS JSON commands through a private relay/VPN/tunnel.
- Discord command surface using slash commands/buttons, owner allowlist, Discord request signature verification, ephemeral replies for sensitive commands, and no SMS push by default.
- Agent-executed QA using `adb`, `curl`, `logcat`, `dumpsys`, Android instrumentation/unit tests where useful, and saved evidence under `.omo/evidence/`.
- Documentation for install, update, rollback, permission grant flow, Shortcuts setup, Discord setup, safety behavior, and no-public-port verification.

### Must NOT have (guardrails, anti-slop, scope boundaries)
- No disabling, patching, or bypassing Samsung/Android thermal mitigation.
- No killing thermal daemons, editing `/vendor/etc/thermal*`, changing thermal HAL behavior, or forcing NR/5G while Android thermal status is unsafe.
- No public unauthenticated API on the phone; no binding to `0.0.0.0` by default.
- No SMS forwarding to Discord or iPhone by default; SMS is pull-only, redacted by default, owner-only, and rate-limited.
- No carrier provisioning bypass, hidden APN fraud, IMEI/baseband/SIM/eSIM modification, modem firmware patching, or attempts to defeat carrier restrictions.
- No reliance on one hard-coded OneUI command before collecting device evidence.
- No broad SELinux permissive mode. Add narrow `sepolicy.rule` only when a failing on-device denial proves it is required.

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: TDD where seams are local and deterministic: API auth/allowlist/rate limit, JSON schema, SMS redaction, Discord signature verifier, Shortcut payloads, thermal policy state machine. Tests-after for Magisk packaging and Android helper wiring where device/emulator feedback is required. Manual QA always required on actual Z Flip 5 before done.
- Static checks: shellcheck if shell scripts exist, Gradle unit/instrumentation tests if helper APK uses Gradle, `ktlint`/Android lint if configured, JSON schema validation for API payloads.
- Real-surface evidence:
  - `adb shell su -c 'ls -la /data/adb/modules/<module_id> && logcat -d -s zflip5-modem'`
  - `adb forward tcp:18080 tcp:<daemon_port>` then `curl -i http://127.0.0.1:18080/v1/status`
  - unauthenticated `curl -i` must return `401`.
  - forbidden remote bind check: `adb shell su -c 'ss -ltnp | grep <daemon_port>'` must show loopback only unless private tunnel mode explicitly enabled.
  - helper API/instrumentation must return real thermal/battery/telephony/SMS permission states.
  - Discord webhook verifier tests must reject bad signatures and accept a signed fixture.
  - Shortcuts examples must be validated by replaying exact HTTP request payloads with `curl`.
- Evidence naming:
  - `.omo/evidence/task-1-zflip5-modem-module.txt` through `.omo/evidence/task-14-zflip5-modem-module.txt`
  - `.omo/evidence/final-zflip5-modem-module-*.txt`

## Execution strategy
### Parallel execution waves
- Wave 1: Discovery and contracts. Todos 1-3 define the real device facts, safety policy, API schema, and test harness. These block all implementation choices that depend on OneUI behavior.
- Wave 2: Core local system. Todos 4-7 build Magisk packaging, daemon, helper APK, and status collectors. Todo 4 and Todo 5 can start after Todo 2; Todo 6 depends on helper skeleton; Todo 7 depends on policy contract.
- Wave 3: Control surfaces. Todos 8-11 build iPhone Shortcuts, Discord relay, SMS command, and tether/5G commands against the local API.
- Wave 4: Hardening and docs. Todos 12-14 add install/rollback docs, security hardening, and full on-device QA.
- Final wave: independent audits and real-surface verification.

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1. Device discovery script | none | 2, 4, 6, 7, 11, 14 | none |
| 2. Contracts and threat model | 1 | 4, 5, 8, 9, 10, 11, 12 | 3 |
| 3. Test harness skeleton | none | all tests/evidence | 2 |
| 4. Magisk package and boot service | 1, 2, 3 | 5, 14 | 6 |
| 5. Local daemon API/security | 2, 3, 4 | 8, 9, 10, 11, 12, 14 | 6, 7 |
| 6. Android helper APK/service | 1, 2, 3 | 7, 10, 11, 14 | 4, 5 |
| 7. Status collectors | 1, 2, 5, 6 | 8, 9, 11, 14 | none after deps |
| 8. iPhone Shortcuts examples | 5, 7 | 14 | 9, 10 |
| 9. Discord interaction relay | 2, 5, 7 | 14 | 8, 10 |
| 10. SMS reader command | 2, 5, 6 | 9, 14 | 8 |
| 11. Tether and safe 5G controls | 1, 5, 6, 7 | 14 | 12 |
| 12. Security hardening | 5, 8, 9, 10, 11 | 14 | 13 |
| 13. Docs and rollback | 4, 8, 9, 10, 11 | 14 | 12 |
| 14. Full device QA | all previous | final verification | none |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->

- [ ] 1. Create device discovery and capability inventory
  What to do / Must NOT do: Create read-only discovery scripts under the project, for example `tools/discover-device.sh` and `tools/collect-capabilities.sh`, that run through `adb` and collect OneUI firmware, Android version, Magisk version, root availability, SIM/subscription IDs, `getprop`, `settings list global|secure|system` relevant to mobile/tethering, `cmd phone` help/output, `dumpsys telephony.registry`, `dumpsys connectivity`, `dumpsys battery`, thermal zones, `service list`, and current listening ports. Must NOT change settings, start/stop tethering, alter radio state, read SMS bodies, or write system/vendor files.
  Parallelization: Wave 1 | Blocked by: none | Blocks: 2, 4, 6, 7, 11, 14
  References (executor has NO interview context - be exhaustive): `.omo/drafts/zflip5-modem-module.md`; Magisk docs https://topjohnwu.github.io/Magisk/guides.html; Android `TelephonyManager` https://developer.android.com/reference/android/telephony/TelephonyManager; Android thermal mitigation https://source.android.com/docs/core/power/thermal-mitigation; Android `PowerManager` https://developer.android.com/reference/android/os/PowerManager; Android `BatteryManager` https://developer.android.com/reference/android/os/BatteryManager.
  Acceptance criteria (agent-executable): `bash tools/discover-device.sh --adb adb --out .omo/evidence/task-1-zflip5-modem-module.txt` exits 0 with device connected; evidence includes redacted device build, Magisk presence, telephony service facts, thermal zone names, battery temp, subscription IDs, and listening ports; evidence contains no SMS body and no secrets.
  QA scenarios (name exact tool + invocation): Happy: `bash tools/discover-device.sh --adb adb --out .omo/evidence/task-1-zflip5-modem-module.txt`, PASS if file includes `DISCOVERY_OK=1`. Failure: run with disconnected adb target `ADB_SERIAL=missing bash tools/discover-device.sh --out .omo/evidence/task-1-zflip5-modem-module-disconnected.txt`, PASS if exit nonzero and error says device unavailable without partial writes.
  Commit: Y | `chore(discovery): add safe device inventory`

- [ ] 2. Define API contracts, config schema, threat model, and safety policy
  What to do / Must NOT do: Create `docs/architecture.md`, `docs/threat-model.md`, `schemas/api.openapi.json`, `schemas/config.schema.json`, and policy docs for commands, roles, rate limits, token storage, private ingress modes, SMS redaction, and thermal-safe 5G. Must NOT leave any command behavior implicit. Must explicitly state that "force 5G" means safe preference/recovery, not thermal bypass.
  Parallelization: Wave 1 | Blocked by: 1 | Blocks: 4, 5, 8, 9, 10, 11, 12
  References: `.omo/drafts/zflip5-modem-module.md`; Discord interactions docs via Context7 `/discord/discord-api-docs`; Apple Shortcuts API docs https://support.apple.com/guide/shortcuts/request-your-first-api-apd58d46713f/ios; Apple Shortcuts URL docs https://support.apple.com/guide/shortcuts/run-a-shortcut-from-a-url-apd624386f42/ios; Android SMS provider https://developer.android.com/reference/android/provider/Telephony.Sms; Android permissions https://developer.android.com/training/permissions/requesting.
  Acceptance criteria: `python3 -m json.tool schemas/api.openapi.json >/dev/null` and `python3 -m json.tool schemas/config.schema.json >/dev/null`; `rg -n "thermal bypass|kill thermal|0\\.0\\.0\\.0|SMS forwarding by default" docs schemas` returns no unsafe recommendation; `rg -n "prefer/recover NR|unsafe thermal|owner-only|ephemeral|loopback" docs schemas` finds explicit policy.
  QA scenarios: Happy: `python3 tools/validate-contracts.py --schemas schemas --docs docs --out .omo/evidence/task-2-zflip5-modem-module.txt`, PASS if `CONTRACTS_OK=1`. Failure: run validator against a fixture config missing auth token and using `bind_host=0.0.0.0`, PASS if validator rejects it and records `PUBLIC_BIND_REJECTED=1`.
  Commit: Y | `docs(contracts): define modem API guardrails`

- [ ] 3. Build local test and evidence harness
  What to do / Must NOT do: Add project test runner wrappers and `.omo/evidence` creation helpers. Include shell tests for scripts, unit tests for policy and JSON redaction, signature fixture tests, and command replay fixtures. Must NOT require human copy/paste for evidence.
  Parallelization: Wave 1 | Blocked by: none | Blocks: all tests/evidence
  References: `.omo/drafts/zflip5-modem-module.md`; Discord Ed25519 verification docs via Context7 `/discord/discord-api-docs`; Android docs listed in Todo 1.
  Acceptance criteria: `bash tools/run-tests.sh --offline --out .omo/evidence/task-3-zflip5-modem-module.txt` exits 0 on host without Android device by running only offline tests; output lists skipped device tests as skipped, not passed.
  QA scenarios: Happy: `bash tools/run-tests.sh --offline --out .omo/evidence/task-3-zflip5-modem-module.txt`, PASS if `OFFLINE_TESTS_OK=1`. Failure: corrupt one JSON fixture in a temp copy and run validator, PASS if exit nonzero and evidence names invalid fixture.
  Commit: Y | `test(harness): add offline and evidence runner`

- [ ] 4. Implement Magisk module packaging and boot lifecycle
  What to do / Must NOT do: Add module files: `module.prop`, `customize.sh` or installer script if needed, `service.sh`, optional `post-fs-data.sh` only if a proven early hook is required, `uninstall.sh`, default config, log directories, and service supervision. `service.sh` must start daemon in late_start mode, wait for boot completion if needed, and never block boot. Must NOT use broad SELinux permissive mode or early `setprop` deadlock-prone patterns.
  Parallelization: Wave 2 | Blocked by: 1, 2, 3 | Blocks: 5, 14
  References: Magisk module docs https://topjohnwu.github.io/Magisk/guides.html; `.omo/drafts/zflip5-modem-module.md`.
  Acceptance criteria: `bash tools/package-module.sh --out dist/zflip5-modem-module.zip` creates valid zip with required Magisk files; `bash tools/lint-magisk-module.sh dist/zflip5-modem-module.zip --out .omo/evidence/task-4-zflip5-modem-module.txt` confirms required files, no unsafe `setprop` in `post-fs-data`, no permissive SELinux, and uninstall cleanup present.
  QA scenarios: Happy: `adb push dist/zflip5-modem-module.zip /sdcard/ && adb shell su -c 'magisk --install-module /sdcard/zflip5-modem-module.zip'` then reboot and capture `adb shell su -c 'ls -la /data/adb/modules/zflip5_modem && logcat -d -s zflip5-modem' > .omo/evidence/task-4-zflip5-modem-module.txt`, PASS if module enabled and service log says started. Failure: install corrupt zip fixture, PASS if Magisk rejects and device still boots with no module directory.
  Commit: Y | `feat(magisk): add module lifecycle`

- [ ] 5. Implement authenticated loopback daemon API
  What to do / Must NOT do: Add daemon process with config loading, JSON API, token auth, command allowlist, loopback bind default, structured logs, health endpoint, error schema, and rate limits. Choose small runtime compatible with Android root environment; if using a binary, document build/ABI. Must NOT bind public interfaces by default or expose shell execution.
  Parallelization: Wave 2 | Blocked by: 2, 3, 4 | Blocks: 8, 9, 10, 11, 12, 14
  References: `schemas/api.openapi.json`; `schemas/config.schema.json`; `.omo/drafts/zflip5-modem-module.md`.
  Acceptance criteria: Offline unit tests prove `GET /v1/status` without token returns `401`, bad token returns `401`, valid token returns schema-valid JSON, unknown command returns `404/405`, rate limit returns `429`, and config with `bind_host=0.0.0.0` is rejected unless explicit private-tunnel mode is enabled.
  QA scenarios: Happy: `adb forward tcp:18080 tcp:<daemon_port> && curl -i -H "Authorization: Bearer $TOKEN" http://127.0.0.1:18080/v1/status > .omo/evidence/task-5-zflip5-modem-module.txt`, PASS if HTTP 200 and schema-valid JSON. Failure: `curl -i http://127.0.0.1:18080/v1/status >> .omo/evidence/task-5-zflip5-modem-module.txt`, PASS if HTTP 401 and no sensitive fields.
  Commit: Y | `feat(daemon): add authenticated local API`

- [ ] 6. Implement Android helper APK/service for framework APIs
  What to do / Must NOT do: Create helper APK/service with explicit permissions and Binder/local IPC or localhost bridge to daemon. Implement readers for telephony, subscription, battery, thermal, and SMS permission state. Include permission request/default SMS handler UX if required. Must NOT silently grant or bypass runtime permissions except through documented root install helper where explicitly safe and reversible.
  Parallelization: Wave 2 | Blocked by: 1, 2, 3 | Blocks: 7, 10, 11, 14
  References: Android `TelephonyManager` https://developer.android.com/reference/android/telephony/TelephonyManager; Android `SubscriptionManager` https://developer.android.com/reference/android/telephony/SubscriptionManager; Android `PowerManager` https://developer.android.com/reference/android/os/PowerManager; Android `BatteryManager` https://developer.android.com/reference/android/os/BatteryManager; Android SMS provider https://developer.android.com/reference/android/provider/Telephony.Sms; Android permissions https://developer.android.com/training/permissions/requesting.
  Acceptance criteria: `./gradlew test` or chosen helper test command exits 0; instrumentation command returns JSON with permission state and synthetic mocked providers in unit tests; helper gracefully reports missing SMS permission/default-handler state without crashing.
  QA scenarios: Happy: `adb shell am instrument -w <helper_package>.test/androidx.test.runner.AndroidJUnitRunner > .omo/evidence/task-6-zflip5-modem-module.txt`, PASS if instrumentation reports OK and helper service responds to status probe. Failure: revoke SMS permission/default role then run SMS permission probe, PASS if helper returns `sms.available=false` with actionable reason and no stacktrace.
  Commit: Y | `feat(helper): add Android framework bridge`

- [ ] 7. Implement status collectors and normalized status payload
  What to do / Must NOT do: Combine daemon and helper facts into `/v1/status`, `/v1/network`, `/v1/thermal`, and `/v1/battery`. Normalize network type names, NR/5G display info when available, signal, SIM/carrier, IP/tethering, Android thermal status, battery temp/level/plugged, and service health. Must NOT claim 5G unless Android telephony data supports it; distinguish LTE, LTE+ display, NR NSA, NR SA, unknown.
  Parallelization: Wave 2 | Blocked by: 1, 2, 5, 6 | Blocks: 8, 9, 11, 14
  References: Android `TelephonyDisplayInfo` and `TelephonyManager` docs; Android `ConnectivityManager`/network capabilities if used; Android `PowerManager`; Android `BatteryManager`; discovery evidence from Todo 1.
  Acceptance criteria: Unit tests map mocked Android network/thermal/battery states into stable JSON; JSON validates against API schema; unknown/missing fields are represented as `null` plus `source`/`available=false`, not guessed.
  QA scenarios: Happy: `curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:18080/v1/status | tee .omo/evidence/task-7-zflip5-modem-module.json | python3 tools/validate-status.py`, PASS if schema-valid and includes `network`, `thermal`, `battery`, `service`. Failure: stop helper service and call status, PASS if daemon returns degraded status with `helper.available=false`, not HTTP 500.
  Commit: Y | `feat(status): normalize modem telemetry`

- [ ] 8. Add iPhone Shortcuts command examples and replay tests
  What to do / Must NOT do: Add documented Shortcuts JSON examples and importable descriptions for status, thermal, battery, network, SMS recent, tether on/off/status, prefer5g, cooldown, and restart-service. Use HTTPS endpoint through private relay/VPN/tunnel; include headers, method, JSON body, and expected response. Must NOT require public phone port or embed secrets in screenshots/docs.
  Parallelization: Wave 3 | Blocked by: 5, 7 | Blocks: 14
  References: Apple Shortcuts URL scheme https://support.apple.com/guide/shortcuts/run-a-shortcut-from-a-url-apd624386f42/ios; Apple Shortcuts API request guide https://support.apple.com/guide/shortcuts/request-your-first-api-apd58d46713f/ios; `schemas/api.openapi.json`.
  Acceptance criteria: `bash tools/replay-shortcuts.sh --base-url http://127.0.0.1:18080 --token "$TOKEN" --out .omo/evidence/task-8-zflip5-modem-module.txt` replays every documented Shortcut request and validates status codes/JSON; docs show how to store token in Shortcut variable, not hard-code it in shared text.
  QA scenarios: Happy: replay all Shortcut examples against local daemon, PASS if every request returns expected status. Failure: replay with missing token, PASS if every sensitive endpoint returns 401 and evidence contains no SMS body.
  Commit: Y | `docs(shortcuts): add iPhone control recipes`

- [ ] 9. Add Discord slash-command relay with signature verification
  What to do / Must NOT do: Build small relay service or serverless handler for Discord interactions. Support `/modem status`, `/modem thermal`, `/modem battery`, `/modem network`, `/modem sms recent`, `/modem tether`, `/modem prefer5g`, `/modem cooldown`, and buttons where useful. Verify `X-Signature-Ed25519` and `X-Signature-Timestamp`, handle PING, restrict by user ID/guild allowlist, use ephemeral replies for SMS/network-sensitive outputs, and call phone daemon only through private ingress. Must NOT run an unverified webhook or leak SMS into public channels.
  Parallelization: Wave 3 | Blocked by: 2, 5, 7, 10 for SMS command | Blocks: 14
  References: Discord Context7 `/discord/discord-api-docs`: interactions via Gateway or outgoing webhook are mutually exclusive; outgoing webhooks require Ed25519 verification; PING must be answered; direct JSON responses and ephemeral flags are supported.
  Acceptance criteria: Unit tests with signed and tampered fixtures prove PING response, good slash command response, bad signature 401, unauthorized user denial, and SMS reply marked ephemeral/redacted. Relay config validates allowlist and phone endpoint.
  QA scenarios: Happy: `bash tools/test-discord-relay.sh --signed-fixtures fixtures/discord --out .omo/evidence/task-9-zflip5-modem-module.txt`, PASS if `DISCORD_RELAY_OK=1`. Failure: replay fixture with modified body/signature, PASS if HTTP 401 and relay does not call phone daemon mock.
  Commit: Y | `feat(discord): add secure modem commands`

- [ ] 10. Implement SMS recent reader with redaction and owner-only access
  What to do / Must NOT do: Implement `/v1/sms/recent` through helper APK and daemon policy. Return sender, timestamp, SIM/subscription if available, and redacted body preview by default. Full body requires explicit `include_body=true` and stronger owner-only command path if added. Add max count, age limit, rate limit, audit log, and empty/no-permission states. Must NOT forward SMS automatically or include one-time codes by default if redaction can detect common OTP patterns.
  Parallelization: Wave 3 | Blocked by: 2, 5, 6 | Blocks: 9, 14
  References: Android SMS provider https://developer.android.com/reference/android/provider/Telephony.Sms; Android permissions https://developer.android.com/training/permissions/requesting; `.omo/drafts/zflip5-modem-module.md`.
  Acceptance criteria: Unit tests prove redaction of OTP-like bodies, count/age caps, permission denied response, auth required, rate limit, and audit logging. Instrumentation test reads a seeded/mock provider where possible; on real phone with no permission it returns a safe no-permission result.
  QA scenarios: Happy: `curl -s -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:18080/v1/sms/recent?limit=3" > .omo/evidence/task-10-zflip5-modem-module.json`, PASS if schema-valid, redacted, max 3, no unredacted OTP pattern. Failure: call without token and with over-limit count, PASS if 401/400 and no SMS content.
  Commit: Y | `feat(sms): add private recent-message reader`

- [ ] 11. Implement tethering and safe 5G preference/recovery controls
  What to do / Must NOT do: Use Todo 1 discovery to select the safest documented/observed command path for tethering status/on/off and network preference. Implement policy state machine: SAFE, WARM, HOT, COOLDOWN, RECOVERY. `/prefer5g` may request allowed/preferred NR-capable modes only when thermal state is safe/warm below cutoff; at/above unsafe thermal status or high battery temp it must refuse, alert, or enter cooldown. Detect 4G fallback and retry only after cooldown. Must NOT override thermal services, patch vendor config, or force radio while unsafe.
  Parallelization: Wave 3 | Blocked by: 1, 5, 6, 7 | Blocks: 14
  References: Android `TelephonyManager` network type docs https://developer.android.com/reference/android/telephony/TelephonyManager; Android `PowerManager` thermal docs https://developer.android.com/reference/android/os/PowerManager; AOSP thermal mitigation https://source.android.com/docs/core/power/thermal-mitigation; discovery evidence from Todo 1.
  Acceptance criteria: Policy tests prove safe prefer5g allowed, 44 C warning recorded, unsafe thermal denies radio preference, fallback triggers cooldown, cooldown prevents retries, recovery retries only when safe. On-device command tests record selected tether/network command and before/after status without unsafe thermal state.
  QA scenarios: Happy: `curl -i -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:18080/v1/network/prefer5g > .omo/evidence/task-11-zflip5-modem-module.txt`, PASS if safe state returns accepted/applied or explains carrier/device constraint. Failure: run policy fixture `THERMAL_STATUS=severe BATTERY_TEMP_C=45 bash tools/test-5g-policy.sh`, PASS if command is denied and evidence says thermal safety gate blocked it.
  Commit: Y | `feat(radio): add thermal-aware 5g policy`

- [ ] 12. Harden secrets, transport, logs, and public exposure checks
  What to do / Must NOT do: Add token generation, config permissions, log redaction, audit logs, owner allowlist validation, rate limits, private relay/VPN/tunnel mode docs, and automated port exposure checks. Must NOT print tokens in logs/evidence or store secrets in world-readable paths.
  Parallelization: Wave 4 | Blocked by: 5, 8, 9, 10, 11 | Blocks: 14
  References: `docs/threat-model.md`; Discord docs via Context7; `.omo/drafts/zflip5-modem-module.md`.
  Acceptance criteria: `bash tools/security-check.sh --out .omo/evidence/task-12-zflip5-modem-module.txt` verifies config file permissions, no token leakage in logs, daemon loopback bind, rate-limit behavior, and redacted evidence. Unit tests prove secrets redacted from error logs.
  QA scenarios: Happy: run security check on installed device, PASS if `SECURITY_CHECK_OK=1`. Failure: temporarily set bind host fixture to `0.0.0.0`, PASS if config validator rejects before daemon starts.
  Commit: Y | `fix(security): harden modem control surface`

- [ ] 13. Write install, update, rollback, and operation docs
  What to do / Must NOT do: Add `README.md`, `docs/install.md`, `docs/shortcuts.md`, `docs/discord.md`, `docs/safety.md`, `docs/troubleshooting.md`, and rollback docs. Include exact commands, prerequisite list, tested device/firmware fields from discovery, permission flow, no-public-port check, how to disable module from recovery/Magisk, and what 5G safety policy does. Must NOT imply unsupported always-5G thermal bypass.
  Parallelization: Wave 4 | Blocked by: 4, 8, 9, 10, 11 | Blocks: 14
  References: all previous docs and evidence; Magisk docs; Apple Shortcuts docs; Discord docs; Android docs.
  Acceptance criteria: `bash tools/check-docs.sh --out .omo/evidence/task-13-zflip5-modem-module.txt` validates command snippets for known scripts, required warnings present, no unsafe bypass language, and no placeholder tokens/secrets.
  QA scenarios: Happy: run docs checker, PASS if `DOCS_OK=1`. Failure: fixture doc containing `kill thermal` or public bind instruction is rejected by checker.
  Commit: Y | `docs: document modem module setup`

- [ ] 14. Run full on-device QA and package release candidate
  What to do / Must NOT do: Install fresh module on Z Flip 5, grant helper permissions through documented path, start service, run all API calls, replay Shortcuts requests, run Discord relay fixtures, test SMS permission states, test tether status/toggle if carrier/device allows, test safe 5G policy including unsafe fixture denial, reboot once, verify service persistence, uninstall/rollback, and package release candidate. Must NOT claim carrier/tether/5G actions succeeded unless before/after evidence proves it on device.
  Parallelization: Wave 4 | Blocked by: all previous | Blocks: final verification
  References: all previous todos; `.omo/drafts/zflip5-modem-module.md`; `.omo/plans/zflip5-modem-module.md`.
  Acceptance criteria: `bash tools/full-device-qa.sh --serial <zflip5_serial> --out-dir .omo/evidence` exits 0 or records explicit pre-existing/device/carrier blockers; evidence includes install, service start, API auth failure/pass, status payload, thermal/battery/network payloads, SMS redaction/no-permission state, loopback-only port, Shortcuts replay, Discord fixture, reboot persistence, and uninstall cleanup.
  QA scenarios: Happy: `bash tools/full-device-qa.sh --serial <zflip5_serial> --out-dir .omo/evidence`, PASS if `FULL_DEVICE_QA_OK=1`. Failure: run with invalid token and simulated unsafe thermal fixture, PASS if every sensitive command is denied and no radio command executes.
  Commit: Y | `chore(release): verify module on device`

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [ ] F1. Plan compliance audit: compare final implementation against every Must have/Must NOT have item in this plan. Evidence `.omo/evidence/final-zflip5-modem-module-plan-compliance.txt`. PASS only if no todo or guardrail missing.
- [ ] F2. Code quality review: inspect daemon, helper APK, scripts, relay, schemas, and docs for smallest-correct implementation, secret handling, test coverage, and maintainability. Evidence `.omo/evidence/final-zflip5-modem-module-code-review.txt`. PASS only with no blocking findings.
- [ ] F3. Real manual QA: rerun full device QA on actual Z Flip 5 plus a clean install/uninstall/reboot cycle. Evidence `.omo/evidence/final-zflip5-modem-module-manual-qa.txt`. PASS only if matching surface behavior works or blocker is proven external/pre-existing.
- [ ] F4. Scope fidelity: verify no thermal bypass, public root API, SMS auto-forwarding, carrier bypass, baseband/IMEI/SIM changes, or broad SELinux permissive mode landed. Evidence `.omo/evidence/final-zflip5-modem-module-scope-fidelity.txt`. PASS only if all forbidden patterns are absent and device checks confirm loopback bind.

## Commit strategy
- One commit per todo, in order where dependencies require it.
- Conventional Commits:
  - `chore(discovery): add safe device inventory`
  - `docs(contracts): define modem API guardrails`
  - `test(harness): add offline and evidence runner`
  - `feat(magisk): add module lifecycle`
  - `feat(daemon): add authenticated local API`
  - `feat(helper): add Android framework bridge`
  - `feat(status): normalize modem telemetry`
  - `docs(shortcuts): add iPhone control recipes`
  - `feat(discord): add secure modem commands`
  - `feat(sms): add private recent-message reader`
  - `feat(radio): add thermal-aware 5g policy`
  - `fix(security): harden modem control surface`
  - `docs: document modem module setup`
  - `chore(release): verify module on device`
- Do not commit automatically unless user explicitly asks. If committing later, final commit body/footer should include `Plan: .omo/plans/zflip5-modem-module.md`.

## Success criteria
- Module installs and uninstalls cleanly on the Z Flip 5 through Magisk.
- Root daemon starts after boot, binds loopback by default, authenticates every sensitive command, rate-limits calls, and logs safely.
- iPhone Shortcuts can query status and send supported actions through private HTTPS ingress.
- Discord slash commands can query/control modem through verified interactions, owner allowlist, and ephemeral sensitive replies.
- Status outputs include network, thermal, battery, service health, and SMS permission/reader state with schema-valid JSON.
- SMS recent is pull-only, owner-only, redacted by default, rate-limited, and never pushed automatically.
- Tether and 5G commands use device-discovered safe command paths, report carrier/device constraints honestly, and never bypass thermal protection.
- Thermal policy treats 44 C as warning, Android thermal unsafe status as hard block, and recovers 5G preference only after cooldown/safe state.
- Full on-device QA evidence exists for install, API auth pass/fail, status surfaces, control commands, safety denials, reboot persistence, and rollback.
