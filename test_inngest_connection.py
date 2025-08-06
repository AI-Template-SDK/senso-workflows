#!/usr/bin/env python3
"""
Test script to verify Inngest connection and function registration
"""

import aiohttp
import asyncio
import json

async def test_inngest_connection():
    """Test if Inngest dev server is running and accessible"""
    url = "http://localhost:8288"
    
    try:
        async with aiohttp.ClientSession() as session:
            # Test basic connectivity
            async with session.get(f"{url}/") as resp:
                print(f"Inngest server status: {resp.status}")
                if resp.status == 200:
                    print("✅ Inngest dev server is running")
                else:
                    print("❌ Inngest dev server returned unexpected status")
                    return False
            
            # Test function registration endpoint
            async with session.get(f"{url}/api/functions") as resp:
                print(f"Functions endpoint status: {resp.status}")
                content_type = resp.headers.get('content-type', '')
                print(f"Content-Type: {content_type}")
                
                if resp.status == 200:
                    if 'application/json' in content_type:
                        functions = await resp.json()
                        print(f"✅ Found {len(functions)} registered functions:")
                        for func in functions:
                            print(f"  - {func.get('name', 'Unknown')} (ID: {func.get('id', 'Unknown')})")
                    else:
                        # Got HTML instead of JSON - functions not registered
                        html_content = await resp.text()
                        print("❌ Functions endpoint returned HTML instead of JSON")
                        print("This means your Go functions are not registered with Inngest")
                        print("\nTo fix this:")
                        print("1. Make sure your Go app is running")
                        print("2. Check that your Go app is connecting to Inngest")
                        print("3. Verify your INNGEST_EVENT_KEY is set correctly")
                        return False
                else:
                    print("❌ Could not fetch functions")
                    return False
                    
    except aiohttp.ClientConnectorError:
        print("❌ Could not connect to Inngest dev server")
        print("Make sure to run: npx inngest-cli@latest dev")
        return False
    except Exception as e:
        print(f"❌ Error testing connection: {e}")
        return False
    
    return True

async def test_event_sending():
    """Test sending a simple event"""
    url = "http://localhost:8288/api/event"
    payload = {
        "name": "org.process",
        "data": {
            "org_id": "test-org-123",
            "triggered_by": "test"
        }
    }
    
    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(url, json=payload) as resp:
                print(f"Event sending status: {resp.status}")
                if resp.status == 200:
                    result = await resp.json()
                    print(f"✅ Event sent successfully: {result}")
                    return True
                else:
                    error_text = await resp.text()
                    print(f"❌ Failed to send event: {error_text}")
                    return False
    except Exception as e:
        print(f"❌ Error sending event: {e}")
        return False

async def main():
    print("Testing Inngest connection...")
    print("=" * 50)
    
    # Test 1: Check if Inngest server is running
    if not await test_inngest_connection():
        print("\n❌ Inngest connection test failed")
        return
    
    print("\n" + "=" * 50)
    
    # Test 2: Try sending a test event
    print("Testing event sending...")
    if await test_event_sending():
        print("\n✅ All tests passed! Your Inngest setup is working correctly.")
    else:
        print("\n❌ Event sending test failed")
        print("This might indicate:")
        print("1. Function not properly registered")
        print("2. Event format issues")
        print("3. Inngest configuration problems")

if __name__ == "__main__":
    asyncio.run(main()) 