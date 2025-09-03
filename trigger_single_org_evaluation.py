import requests
import json
import sys

TRIGGERED_BY = "manual_test"
USER_ID = "test-user"
INNGEST_URL = "http://localhost:8288/e/dev"


def trigger_single_org_evaluation(org_id):
    """
    Triggers the org evaluation pipeline for a single organization.
    
    This is useful for testing and debugging the new pipeline.
    """
    payload = {
        "name": "org.evaluation.process",
        "data": {
            "org_id": org_id,
            "triggered_by": TRIGGERED_BY,
            "user_id": USER_ID
        }
    }
    
    headers = {"Content-Type": "application/json"}
    
    print("=" * 60)
    print("🧪 SINGLE ORG EVALUATION TRIGGER")
    print("=" * 60)
    print(f"🎯 Organization ID: {org_id}")
    print(f"📡 Event: org.evaluation.process")
    print(f"🔗 Endpoint: {INNGEST_URL}")
    print(f"👤 Triggered by: {TRIGGERED_BY}")
    print(f"👤 User ID: {USER_ID}")
    print("=" * 60)
    
    try:
        print(f"🚀 Sending trigger request...")
        response = requests.post(INNGEST_URL, json=payload, headers=headers)
        
        if response.status_code == 200:
            print(f"✅ SUCCESS | Status: {response.status_code}")
            print(f"📄 Response: {response.json()}")
            print("\n🎉 Org evaluation pipeline triggered successfully!")
            print("📊 Check the workflow logs to monitor progress.")
        else:
            print(f"❌ FAILED | Status: {response.status_code}")
            print(f"📄 Response: {response.text}")
            
    except requests.exceptions.RequestException as e:
        print(f"❌ ERROR | Failed to make request: {e}")
    
    print("=" * 60)


def main():
    """
    Main function to trigger org evaluation for a single org.
    
    Usage:
        python trigger_single_org_evaluation.py <org_id>
        
    Example:
        python trigger_single_org_evaluation.py 123e4567-e89b-12d3-a456-426614174000
    """
    if len(sys.argv) != 2:
        print("❌ Usage: python trigger_single_org_evaluation.py <org_id>")
        print("📝 Example: python trigger_single_org_evaluation.py 123e4567-e89b-12d3-a456-426614174000")
        sys.exit(1)
    
    org_id = sys.argv[1].strip()
    
    if not org_id:
        print("❌ Error: org_id cannot be empty")
        sys.exit(1)
    
    trigger_single_org_evaluation(org_id)


if __name__ == "__main__":
    main() 