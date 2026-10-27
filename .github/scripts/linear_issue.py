"""Create a Linear issue for a Dependabot pull request.

Reads everything from environment variables so the PR title is never
interpolated into a shell command.
"""

import json
import os
import sys
import urllib.error
import urllib.request

API_URL = "https://api.linear.app/graphql"

MUTATION = """
mutation($input: IssueCreateInput!) {
  issueCreate(input: $input) { success issue { identifier url } }
}
"""


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
        with urllib.request.urlopen(request, timeout=30) as response:
            body = json.load(response)
    except urllib.error.HTTPError as err:
        print(f"Linear returned HTTP {err.code}: {err.read().decode()}", file=sys.stderr)
        return 1

    result = (body.get("data") or {}).get("issueCreate") or {}
    if not result.get("success"):
        print(f"Linear did not create the issue: {body}", file=sys.stderr)
        return 1

    print(result["issue"]["url"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
