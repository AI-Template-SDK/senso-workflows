#!/usr/bin/env python3
"""
Trigger enhanced network org re-evaluation workflow for all orgs in example_orgs.txt
This will re-evaluate ALL network question runs for each org using the enhanced org evaluation methodology.
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

def trigger_network_org_reeval_enhanced(org_id, inngest_url="http://localhost:8288/e/dev"):
    """Trigger enhanced network org re-evaluation workflow for a single org"""
    event_data = {
        "name": "network.org.reeval.enhanced",
        "data": {
            "org_id": org_id,
            "triggered_by": "enhanced_bulk_trigger_script"
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
            print(f"✅ Successfully triggered enhanced network org re-evaluation for org: {org_id}")
            return True
        else:
            print(f"❌ Failed to trigger enhanced network org re-evaluation for org {org_id}: {response.status_code} - {response.text}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Error triggering enhanced network org re-evaluation for org {org_id}: {e}")
        return False

def main():
    print("🚀 Starting Enhanced Network Org Re-evaluation Workflow Trigger...")
    print("=" * 70)
    print("🔄 Enhanced Network Org Re-evaluation Workflow")
    print("=" * 70)
    print("This enhanced workflow will:")
    print("• Process ALL network question runs for each organization (not just latest)")
    print("• Use sophisticated org evaluation methodology with name variations")
    print("• Delete existing network_org_* data before saving new results")
    print("• Apply enhanced analysis for better competitive intelligence")
    print("• Save results to network_org_evals, network_org_competitors, network_org_citations")
    print("=" * 70)
    
    # Read org IDs
    org_ids = read_org_ids()
    if not org_ids:
        print("❌ No org IDs found in example_orgs.txt")
        sys.exit(1)
    
    print(f"📊 Found {len(org_ids)} orgs to process:")
    for i, org_id in enumerate(org_ids, 1):
        print(f"  {i}. {org_id}")
    
    # Confirm before proceeding
    print(f"\n⚠️  ENHANCED METHODOLOGY NOTICE:")
    print(f"   This uses the sophisticated org evaluation methodology")
    print(f"   applied to network questions for enhanced analysis.")
    print(f"   This will trigger enhanced re-evaluation for {len(org_ids)} orgs.")
    
    confirm = input(f"\nContinue with enhanced network org re-evaluation? (y/N): ")
    if confirm.lower() != 'y':
        print("❌ Cancelled by user")
        sys.exit(0)
    
    # Process each org
    successful = 0
    failed = 0
    
    print(f"\n🚀 Triggering enhanced network org re-evaluation for {len(org_ids)} org(s)...")
    print("-" * 70)
    
    for i, org_id in enumerate(org_ids, 1):
        print(f"\n[{i}/{len(org_ids)}] Processing org: {org_id}")
        
        if trigger_network_org_reeval_enhanced(org_id):
            successful += 1
        else:
            failed += 1
        
        # Add delay between requests to avoid overwhelming the system
        if i < len(org_ids):
            time.sleep(2)
    
    # Summary
    print(f"\n" + "=" * 70)
    print(f"🎉 Enhanced Network Org Re-evaluation Trigger Completed!")
    print(f"✅ Successful: {successful}")
    print(f"❌ Failed: {failed}")
    print(f"📊 Total: {len(org_ids)}")
    print(f"🔬 Methodology: Enhanced Org Evaluation on Network Questions")
    print("=" * 70)
    
    if failed > 0:
        print(f"\n⚠️  {failed} orgs failed to trigger. Check the logs above for details.")
        sys.exit(1)
    else:
        print(f"\n🎊 All {successful} enhanced network org re-evaluation workflows triggered successfully!")
        print(f"📈 Enhanced analysis will provide superior competitive intelligence!")

if __name__ == "__main__":
    main() 