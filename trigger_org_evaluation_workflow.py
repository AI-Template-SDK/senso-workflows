import requests
import json

ORG_FILE = "example_orgs.txt"
TRIGGERED_BY = "manual"
USER_ID = None  # e.g. "user-123" or None
SCHEDULED_DATE = None  # e.g. "2024-07-14" or None
INNGEST_URL = "http://localhost:8288/e/dev"


def trigger_process_org_evaluation(org_id):
    """
    Triggers the new org evaluation pipeline for a specific organization.
    
    This pipeline:
    1. Executes questions with web search for the org
    2. Applies advanced brand analysis methodology from run.py
    3. Extracts org evaluations, competitors, and citations
    4. Stores results in org_evals, org_citations, org_competitors tables
    """
    payload = {
        "name": "org.evaluation.process",
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
            print(f"✅ Org Evaluation: {org_id} | Status: {response.status_code}")
            print(f"Response: {response.json()}")
        else:
            print(f"❌ Org Evaluation: {org_id} | Status: {response.status_code}")
            print(f"Response: {response.text}")
    except requests.exceptions.RequestException as e:
        print(f"❌ Org Evaluation: {org_id} | Error making request: {e}")


def main():
    """
    Main function to trigger org evaluation workflow for all orgs in the file.
    
    The org evaluation pipeline is a new advanced brand analysis system that:
    - Uses the enhanced methodology from Python run.py
    - Processes org-specific questions with web search
    - Extracts substantive brand content and share-of-voice analysis
    - Identifies competitors and classifies citations
    - Runs in parallel to the existing org.process pipeline
    """
    with open(ORG_FILE, "r") as f:
        org_ids = [line.strip() for line in f if line.strip()]
    
    print("=" * 60)
    print("🚀 ORG EVALUATION PIPELINE TRIGGER")
    print("=" * 60)
    print(f"📋 Processing {len(org_ids)} organizations")
    print(f"🎯 Event: org.evaluation.process")
    print(f"🔗 Endpoint: {INNGEST_URL}")
    print(f"👤 Triggered by: {TRIGGERED_BY}")
    print("=" * 60)
    
    for i, org_id in enumerate(org_ids, 1):
        print(f"\n[{i}/{len(org_ids)}] Triggering org evaluation for: {org_id}")
        trigger_process_org_evaluation(org_id)
    
    print("\n" + "=" * 60)
    print("🎉 All org evaluation triggers sent!")
    print("=" * 60)


if __name__ == "__main__":
    main() 