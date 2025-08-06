import requests
import json

ORG_FILE = "example_orgs.txt"
TRIGGERED_BY = "manual"
USER_ID = None  # e.g. "user-123" or None
SCHEDULED_DATE = None  # e.g. "2024-07-14" or None
INNGEST_URL = "http://localhost:8288/e/dev"


def trigger_process_org(org_id):
    payload = {
        "name": "org.process",
        "data": {
            "org_id": org_id,
            "triggered_by": TRIGGERED_BY
        }
    }
    if USER_ID:
        payload["data"]["user_id"] = USER_ID
    if SCHEDULED_DATE:
        payload["data"]["scheduled_date"] = SCHEDULED_DATE
    headers = {"Content-Type": "application/json"}
    try:
        response = requests.post(INNGEST_URL, json=payload, headers=headers)
        if response.status_code == 200:
            print(f"✅ Org: {org_id} | Status: {response.status_code}")
            print(f"Response: {response.json()}")
        else:
            print(f"❌ Org: {org_id} | Status: {response.status_code}")
            print(f"Response: {response.text}")
    except requests.exceptions.RequestException as e:
        print(f"❌ Org: {org_id} | Error making request: {e}")


def main():
    with open(ORG_FILE, "r") as f:
        org_ids = [line.strip() for line in f if line.strip()]
    print(f"Triggering process for {len(org_ids)} orgs...")
    for org_id in org_ids:
        trigger_process_org(org_id)

if __name__ == "__main__":
    main()