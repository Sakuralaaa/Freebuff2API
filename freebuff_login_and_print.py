#!/usr/bin/env python3
"""Generate a Freebuff login URL, poll for completion, and print token/config values."""

from __future__ import annotations

import argparse
import json
import secrets
import sys
import time
import urllib.parse
import urllib.request


CLI_CODE_URL = "https://freebuff.com/api/auth/cli/code"
CLI_STATUS_URL = "https://freebuff.com/api/auth/cli/status"


def api_request(url: str, method: str = "GET", body: dict | None = None) -> dict:
    data = None
    headers = {
        "accept": "application/json",
        "content-type": "application/json",
        "user-agent": "freebuff-login-helper/1.0",
    }
    if body is not None:
        data = json.dumps(body).encode("utf-8")

    request = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(request, timeout=30) as response:
        raw = response.read().decode("utf-8")
    return json.loads(raw)


def build_fingerprint() -> str:
    return "codebuff-cli-" + secrets.token_urlsafe(16)[:26]


def print_config(user: dict, status_payload: dict) -> None:
    auth_token = user.get("authToken") or user.get("auth_token") or ""
    print()
    print("login_success=true")
    print(f"user_name={user.get('name', '')}")
    print(f"user_email={user.get('email', '')}")
    print(f"user_id={user.get('id', '')}")
    print(f"auth_token={auth_token}")
    print(f"fingerprint_id={status_payload.get('fingerprintId', '')}")
    print(f"fingerprint_hash={status_payload.get('fingerprintHash', '')}")
    print(f"expires_at={status_payload.get('expiresAt', '')}")
    print()
    print("pages_secret_FREEBUFF_TOKENS=" + json.dumps([auth_token], ensure_ascii=False))
    print("local_credentials_default=" + json.dumps(
        {
            "id": user.get("id"),
            "name": user.get("name"),
            "email": user.get("email"),
            "authToken": auth_token,
            "fingerprintId": status_payload.get("fingerprintId"),
            "fingerprintHash": status_payload.get("fingerprintHash"),
        },
        ensure_ascii=False,
        indent=2,
    ))


def main() -> int:
    parser = argparse.ArgumentParser(description="Create a Freebuff login link and print the resulting auth token.")
    parser.add_argument("--fingerprint-id", help="Optional custom fingerprintId")
    parser.add_argument("--poll-interval", type=float, default=5.0, help="Polling interval in seconds")
    parser.add_argument("--timeout", type=int, default=300, help="Polling timeout in seconds")
    args = parser.parse_args()

    fingerprint_id = args.fingerprint_id or build_fingerprint()
    create_payload = api_request(CLI_CODE_URL, method="POST", body={"fingerprintId": fingerprint_id})

    login_url = create_payload["loginUrl"]
    fingerprint_hash = create_payload["fingerprintHash"]
    expires_at = create_payload["expiresAt"]

    print(f"fingerprint_id={fingerprint_id}")
    print(f"fingerprint_hash={fingerprint_hash}")
    print(f"expires_at={expires_at}")
    print(f"login_url={login_url}")
    print()
    print("Open the login_url in your browser and complete authorization.")
    print("Polling for completion...")

    deadline = time.time() + args.timeout
    while time.time() < deadline:
        query = urllib.parse.urlencode(
            {
                "fingerprintId": fingerprint_id,
                "fingerprintHash": fingerprint_hash,
                "expiresAt": expires_at,
            }
        )
        try:
            status_payload = api_request(f"{CLI_STATUS_URL}?{query}")
        except Exception as exc:  # noqa: BLE001
            print(f"poll_error={exc}", file=sys.stderr)
            time.sleep(args.poll_interval)
            continue

        user = status_payload.get("user")
        if isinstance(user, dict):
            status_payload["fingerprintId"] = fingerprint_id
            status_payload["fingerprintHash"] = fingerprint_hash
            status_payload["expiresAt"] = expires_at
            print_config(user, status_payload)
            return 0

        time.sleep(args.poll_interval)

    print("login_success=false", file=sys.stderr)
    print("error=timeout_waiting_for_authorization", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
