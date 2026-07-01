#!/usr/bin/env python3
"""validate-contracts.py — Todo 2 contract + safety-policy validator.

Two modes:
  --schemas DIR --docs DIR --out FILE   full check; writes CONTRACTS_OK=1 evidence
  --check-config FILE --schemas DIR      validate one daemon config against the
                                         safety invariants in config.schema.json

No third-party deps (json/re/stdlib only) so it runs anywhere python3 exists.
The config check enforces the load-bearing invariants directly: loopback-only
bind, a present read-status token of the right shape, and the fail-closed /
no-forward / redact-by-default constants — using the pattern/const values read
from the schema so the two stay in sync.
"""
import argparse
import json
import re
import sys

# Safety-policy tokens that MUST appear in docs/schemas, and unsafe
# recommendations that must NOT. Mirrors the plan's Todo 2 acceptance greps.
REQUIRED_TOKENS = ["prefer/recover NR", "unsafe thermal", "owner-only", "ephemeral", "loopback"]
FORBIDDEN = re.compile(r"thermal bypass|kill thermal|0\.0\.0\.0|SMS forwarding by default")


def load_json(path):
    with open(path) as f:
        return json.load(f)


def check_config(config_path, schema_dir):
    """Return (ok, lines). Emits PUBLIC_BIND_REJECTED=1 on a non-loopback bind."""
    lines = []
    ok = True
    schema = load_json(f"{schema_dir}/config.schema.json")
    try:
        cfg = load_json(config_path)
    except (OSError, json.JSONDecodeError) as e:
        return False, [f"CONFIG_PARSE_ERROR: {e}"]

    # bind must match the schema's loopback-only pattern.
    bind_pat = schema["properties"]["bind_host"]["pattern"]
    bind = cfg.get("bind_host", "")
    if not re.match(bind_pat, str(bind)):
        ok = False
        lines.append("PUBLIC_BIND_REJECTED=1")
        lines.append(f"error: bind_host '{bind}' is not loopback-only (pattern {bind_pat})")

    # required read-status token, right shape.
    tok_pat = schema["$defs"]["token"]["pattern"]
    tokens = cfg.get("tokens", {}) or {}
    if "read-status" not in tokens or not tokens.get("read-status"):
        ok = False
        lines.append("error: missing required token 'read-status'")
    else:
        for name, val in tokens.items():
            if not re.match(tok_pat, str(val)):
                ok = False
                lines.append(f"error: token '{name}' too weak (needs >=256-bit hex)")

    # fail-closed / no-forward / redact-by-default constants.
    if cfg.get("thermal", {}).get("fail_closed") is not True:
        ok = False
        lines.append("error: thermal.fail_closed must be true")
    if cfg.get("sms", {}).get("forward") is not False:
        ok = False
        lines.append("error: sms.forward must be false (no default forwarding)")
    if cfg.get("sms", {}).get("redact_default") is not True:
        ok = False
        lines.append("error: sms.redact_default must be true")

    if ok:
        lines.append("CONFIG_OK=1")
    return ok, lines


def full_check(schema_dir, docs_dir):
    lines = []
    ok = True

    for name in ("api.openapi.json", "config.schema.json"):
        try:
            load_json(f"{schema_dir}/{name}")
            lines.append(f"schema_parses:{name}=1")
        except (OSError, json.JSONDecodeError) as e:
            ok = False
            lines.append(f"schema_parses:{name}=0 ({e})")

    # gather docs + schema text
    import glob
    corpus = ""
    for path in glob.glob(f"{docs_dir}/**/*", recursive=True) + glob.glob(f"{schema_dir}/**/*", recursive=True):
        try:
            with open(path) as f:
                corpus += f.read() + "\n"
        except (OSError, IsADirectoryError, UnicodeDecodeError):
            pass

    for tok in REQUIRED_TOKENS:
        if tok in corpus:
            lines.append(f"policy_present:{tok}=1")
        else:
            ok = False
            lines.append(f"policy_present:{tok}=0")

    m = FORBIDDEN.search(corpus)
    if m:
        ok = False
        lines.append(f"UNSAFE_RECOMMENDATION_FOUND={m.group(0)!r}")
    else:
        lines.append("unsafe_recommendation=none")

    lines.append("CONTRACTS_OK=1" if ok else "CONTRACTS_OK=0")
    return ok, lines


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--schemas", required=True)
    ap.add_argument("--docs")
    ap.add_argument("--out")
    ap.add_argument("--check-config")
    args = ap.parse_args()

    if args.check_config:
        ok, lines = check_config(args.check_config, args.schemas)
        out = "\n".join(lines)
        print(out)
        sys.exit(0 if ok else 1)

    if not args.docs:
        ap.error("--docs is required for the full check")
    ok, lines = full_check(args.schemas, args.docs)
    out = "\n".join(lines) + "\n"
    if args.out:
        with open(args.out, "w") as f:
            f.write(out)
    print(out, end="")
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
