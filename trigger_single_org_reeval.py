#!/usr/bin/env python3
"""
Trigger org re-evaluation workflow for a single org.
This will re-evaluate ALL existing question runs for the specified org using the new org evaluation methodology.

Usage:
    python trigger_single_org_reeval.py <org_id>
    python trigger_single_org_reeval.py 7a8862d5-17c8-478c-8b8d-83afed58cdaf
"""

import requests
import json
import sys
import argparse

def trigger_org_reeval(org_id, inngest_url="http://localhost:8288/e/dev"):
    """Trigger org re-evaluation workflow for a single org"""
    event_data = {
        "name": "org.reeval.all.process",
        "data": {
            "org_id": org_id,
            "triggered_by": "single_trigger_script"
        }
    }
    
    try:
        print(f"🚀 Triggering org re-evaluation workflow for org: {org_id}")
        print(f"📋 This will re-evaluate ALL existing question runs for this org")
        print(f"🔗 Sending request to: {inngest_url}")
        
        response = requests.post(
            inngest_url,
            json=event_data,
            headers={"Content-Type": "application/json"},
            timeout=30
        )
        
        if response.status_code == 200:
            print(f"✅ Successfully triggered org re-evaluation workflow!")
            print(f"📊 Org ID: {org_id}")
            print(f"🔄 Event: org.reeval.all.process")
            print(f"📝 Response: {response.text}")
            return True
        else:
            print(f"❌ Failed to trigger org re-evaluation workflow")
            print(f"📊 Status Code: {response.status_code}")
            print(f"📝 Response: {response.text}")
            return False
            
    except requests.exceptions.RequestException as e:
        print(f"❌ Error triggering org re-evaluation workflow: {e}")
        return False

def main():
    parser = argparse.ArgumentParser(
        description="Trigger org re-evaluation workflow for a single org",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    python trigger_single_org_reeval.py 7a8862d5-17c8-478c-8b8d-83afed58cdaf
    python trigger_single_org_reeval.py --url http://production:8080 my-org-id
        """
    )
    
    parser.add_argument(
        "org_id",
        help="The org ID to trigger re-evaluation for"
    )
    
    parser.add_argument(
        "--url",
        default="http://localhost:8288/e/dev",
        help="Inngest service URL (default: http://localhost:8288/e/dev)"
    )
    
    args = parser.parse_args()
    
    if not args.org_id:
        print("❌ Error: org_id is required")
        parser.print_help()
        sys.exit(1)
    
    print("🔍 Org Re-evaluation Workflow Trigger")
    print("=" * 50)
    print(f"📊 Org ID: {args.org_id}")
    print(f"🔗 Service URL: {args.url}")
    print(f"🔄 Event: org.reeval.all.process")
    print("=" * 50)
    
    # Confirm before proceeding
    confirm = input(f"⚠️  This will re-evaluate ALL existing question runs for org {args.org_id}. Continue? (y/N): ")
    if confirm.lower() != 'y':
        print("❌ Cancelled by user")
        sys.exit(0)
    
    success = trigger_org_reeval(args.org_id, args.url)
    
    if success:
        print(f"\n🎉 Org re-evaluation workflow triggered successfully!")
        print(f"💡 You can monitor the progress in the Inngest dashboard")
        print(f"📊 This will process ALL question runs for org: {args.org_id}")
        sys.exit(0)
    else:
        print(f"\n❌ Failed to trigger org re-evaluation workflow")
        sys.exit(1)

if __name__ == "__main__":
    main() 