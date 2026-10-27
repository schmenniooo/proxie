"""Create a Linear issue for a Dependabot pull request.

Reads everything from environment variables so the PR title is never
interpolated into a shell command.
"""

import json
import os
import sys
import time
import urllib.error
import urllib.request

API_URL = "https://api.linear.app/graphql"
MAX_ATTEMPTS = 3
RETRY_DELAY_SECONDS = 2

MUTATION = """
mutation($input: IssueCreateInput!) {
  issueCreate(input: $input) { success issue { identifier url } }
}
"""


def send(request: urllib.request.Request) -> dict:
    """POST the request, retrying network errors, timeouts, 429 and 5xx."""
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as err:
            if err.code < 500 and err.code != 429:
                raise
            failure = err
        except (OSError, ValueError) as err:
            failure = err
        if attempt == MAX_ATTEMPTS:
            raise failure
        delay = RETRY_DELAY_SECONDS * 2 ** (attempt - 1)
        print(f"Attempt {attempt} failed ({failure}), retrying in {delay}s", file=sys.stderr)
        time.sleep(delay)
    raise AssertionError("unreachable")


def main() -> int:
    payload = {
        "query": MUTATION,
        "variables": {
            "input": {
                "teamId": os.environ["TEAM_ID"],
                "projectId": os.environ["PROJECT_ID"],
                "title": f"Review Dependabot PR: {os.environ['PR_TITLE']}",
                "description": f"Dependabot opened {os.environ['PR_URL']}",
            }
        },
    }
    request = urllib.request.Request(
        API_URL,
        data=json.dumps(payload).encode(),
        headers={
            "Content-Type": "application/json",
            "Authorization": os.environ["LINEAR_API_TOKEN"],
        },
    )
    try:
        body = send(request)
    except urllib.error.HTTPError as err:
        print(f"Linear returned HTTP {err.code}: {err.read().decode()}", file=sys.stderr)
        return 1
    except (OSError, ValueError) as err:
        # URLError and timeouts are OSErrors, a non-JSON body is a ValueError.
        print(f"Could not reach Linear or read its reply: {err}", file=sys.stderr)
        return 1

    result = (body.get("data") or {}).get("issueCreate") or {}
    if not result.get("success"):
        print(f"Linear did not create the issue: {body}", file=sys.stderr)
        return 1

    print(result["issue"]["url"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
