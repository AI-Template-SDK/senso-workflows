#!/usr/bin/env python3
"""
Test script to trigger the new network org processing workflow
"""

import requests
import json

# Use a file with org IDs - you can replace this with an actual org ID file
ORG_FILE = "example_orgs.txt"
TRIGGERED_BY = "manual"
USER_ID = None  # e.g. "user-123" or None
INNGEST_URL = "http://localhost:8288/e/dev"

def trigger_process_network_org(org_id):
    payload = {
        "name": "network.org.process",
        "data": {
            "org_id": org_id,
            "triggered_by": TRIGGERED_BY
        }
    }
    if USER_ID:
        payload["data"]["user_id"] = USER_ID

    headers = {"Content-Type": "application/json"}
    try:
        response = requests.post(INNGEST_URL, json=payload, headers=headers)
        if response.status_code == 200:
            print(f"✅ Org: {org_id} | Status: {response.status_code}")
            print(f"Response: {response.json()}")
        else:
            print(f"❌ Org: {org_id} | Status: {response.status_code}")
            print(f"Response: {response.text}")
    except Exception as e:
        print(f"❌ Org: {org_id} | Error triggering network org workflow: {e}")

def main():
    with open(ORG_FILE, "r") as f:
        org_ids = [line.strip() for line in f if line.strip()]
    print(f"Triggering network org processing for {len(org_ids)} orgs...")
    for org_id in org_ids:
        trigger_process_network_org(org_id)

if __name__ == "__main__":
    main()
