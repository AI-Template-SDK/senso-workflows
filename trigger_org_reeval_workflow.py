#!/usr/bin/env python3
"""
Trigger org re-evaluation workflow for all orgs in example_orgs.txt
This will re-evaluate ALL existing question runs for each org using the new org evaluation methodology.
"""

import requests
import json
import time
import sys
from pathlib import Path

def read_org_ids():
    """Read org IDs from example_orgs.txt"""
    org_file = Path("example_orgs.txt")
    if not org_file.exists():
        print("❌ example_orgs.txt not found")
        return []
    
    org_ids = []
    with open(org_file, 'r') as f:
        for line in f:
            line = line.strip()
            if line and not line.startswith('#'):
                org_ids.append(line)
    
    return org_ids

def trigger_org_reeval(org_id, inngest_url="http://localhost:8288/e/dev"):
    """Trigger org re-evaluation workflow for a single org"""
    event_data = {
        "name": "org.reeval.all.process",
        "data": {
            "org_id": org_id,
            "triggered_by": "bulk_trigger_script"
        }
    }
    
    try:
        response = requests.post(
            inngest_url,
            json=event_data,
            headers={"Content-Type": "application/json"},
            timeout=30
        )
        
        if response.status_code == 200:
            print(f"✅ Successfully triggered org re-evaluation for org: {org_id}")
            return True
        else:
            print(f"❌ Failed to trigger org re-evaluation for org {org_id}: {response.status_code} - {response.text}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Error triggering org re-evaluation for org {org_id}: {e}")
        return False

def main():
    print("🚀 Starting bulk org re-evaluation workflow trigger...")
    print("📋 This will re-evaluate ALL existing question runs for each org")
    
    # Read org IDs
    org_ids = read_org_ids()
    if not org_ids:
        print("❌ No org IDs found in example_orgs.txt")
        sys.exit(1)
    
    print(f"📊 Found {len(org_ids)} orgs to process")
    
    # Confirm before proceeding
    confirm = input(f"⚠️  This will trigger re-evaluation for {len(org_ids)} orgs. Continue? (y/N): ")
    if confirm.lower() != 'y':
        print("❌ Cancelled by user")
        sys.exit(0)
    
    # Process each org
    successful = 0
    failed = 0
    
    for i, org_id in enumerate(org_ids, 1):
        print(f"\n[{i}/{len(org_ids)}] Processing org: {org_id}")
        
        if trigger_org_reeval(org_id):
            successful += 1
        else:
            failed += 1
        
        # Add delay between requests to avoid overwhelming the system
        if i < len(org_ids):
            time.sleep(2)
    
    # Summary
    print(f"\n🎉 Bulk org re-evaluation trigger completed!")
    print(f"✅ Successful: {successful}")
    print(f"❌ Failed: {failed}")
    print(f"📊 Total: {len(org_ids)}")
    
    if failed > 0:
        print(f"\n⚠️  {failed} orgs failed to trigger. Check the logs above for details.")
        sys.exit(1)
    else:
        print(f"\n🎊 All {successful} org re-evaluation workflows triggered successfully!")

if __name__ == "__main__":
    main() 