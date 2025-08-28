#!/usr/bin/env python3
"""
Test script to trigger the new network questions workflow
"""

import requests
import json

# Use a file with network IDs - you can replace this with an actual network ID file
NETWORK_FILE = "example_networks.txt"
TRIGGERED_BY = "manual"
USER_ID = None  # e.g. "user-123" or None
INNGEST_URL = "http://localhost:8288/e/dev"

def trigger_process_network(network_id):
    payload = {
        "name": "network.questions.process",
        "data": {
            "network_id": network_id,
            "triggered_by": TRIGGERED_BY
        }
    }
    if USER_ID:
        payload["data"]["user_id"] = USER_ID

    headers = {"Content-Type": "application/json"}
    try:
        response = requests.post(INNGEST_URL, json=payload, headers=headers)
        if response.status_code == 200:
            print(f"✅ Network: {network_id} | Status: {response.status_code}")
            print(f"Response: {response.json()}")
        else:
            print(f"❌ Network: {network_id} | Status: {response.status_code}")
            print(f"Response: {response.text}")
    except Exception as e:
        print(f"❌ Network: {network_id} | Error triggering network workflow: {e}")

def main():
    with open(NETWORK_FILE, "r") as f:
        network_ids = [line.strip() for line in f if line.strip()]
    print(f"Triggering network questions processing for {len(network_ids)} networks...")
    for network_id in network_ids:
        trigger_process_network(network_id)

if __name__ == "__main__":
    main()
