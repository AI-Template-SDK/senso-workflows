#!/usr/bin/env python3
"""
Test script to trigger the new network org re-evaluation workflow
This workflow processes ALL question runs (not just latest) and deletes existing data before saving new results
"""

import requests
import json

# Use a file with org IDs - you can replace this with an actual org ID file
ORG_FILE = "example_orgs.txt"
TRIGGERED_BY = "manual"
USER_ID = None  # e.g. "user-123" or None
INNGEST_URL = "http://localhost:8288/e/dev"

def trigger_process_network_reeval(org_id):
    payload = {
        "name": "network.org.reeval",
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
        print(f"❌ Org: {org_id} | Error triggering network reeval workflow: {e}")

def main():
    print("🔄 Network Org Re-evaluation Workflow Trigger")
    print("=" * 50)
    print("This workflow will:")
    print("• Process ALL question runs for the network (not just latest)")
    print("• Delete existing eval/citation/competitor data before saving new results")
    print("• Re-evaluate all network questions for the specified organization")
    print("=" * 50)
    
    try:
        with open(ORG_FILE, "r") as f:
            org_ids = [line.strip() for line in f if line.strip()]
    except FileNotFoundError:
        print(f"❌ Error: Could not find {ORG_FILE}")
        print("Please create this file with org IDs (one per line) or modify the ORG_FILE variable")
        return
    
    if not org_ids:
        print(f"❌ Error: No org IDs found in {ORG_FILE}")
        return
        
    print(f"📋 Found {len(org_ids)} org(s) to process:")
    for i, org_id in enumerate(org_ids, 1):
        print(f"  {i}. {org_id}")
    
    print(f"\n🚀 Triggering network org re-evaluation for {len(org_ids)} org(s)...")
    print("-" * 50)
    
    for i, org_id in enumerate(org_ids, 1):
        print(f"\n[{i}/{len(org_ids)}] Processing org: {org_id}")
        trigger_process_network_reeval(org_id)
    
    print("\n" + "=" * 50)
    print("✅ All triggers completed!")

if __name__ == "__main__":
    main()
