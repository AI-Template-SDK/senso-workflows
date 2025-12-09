import json
from typing import Any


def describe(value: Any):
    if isinstance(value, dict):
        return {key: describe(val) for key, val in value.items()}
    if isinstance(value, list):
        if not value:
            return {"type": "list", "items": []}
        item_descriptions = []
        for item in value:
            desc = describe(item)
            if desc not in item_descriptions:
                item_descriptions.append(desc)
        return {"type": "list", "items": item_descriptions}
    return type(value).__name__

file = "sd_miqa8pu82fx9ih9fpp.json"

with open(file, "r") as f:
    data = json.load(f)

structure = describe(data)

# Print structure of the data, just the keys and types not the values
print(json.dumps(structure, indent=4, sort_keys=True))

for i in data:
    print(i["aio_text"])
    print("-" * 100)
    print()