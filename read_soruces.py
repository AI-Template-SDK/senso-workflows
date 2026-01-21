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


with open("sd_midqhnodm1qh7hky9.json", "r") as f:
    data = json.load(f)

structure = describe(data)

# Print structure of the data, just the keys and types not the values
print(json.dumps(structure, indent=4, sort_keys=True))

for i in data:
    print(i["prompt"])
    print(i["web_search_triggered"])
    print(len(i.get("links_attached") or []))
    print(len(i.get("citations") or []))
    print(len(i.get("search_sources") or []))
    print(i["model"])
    #print(i["answer_text_markdown"])
    with open(f"sd_{i['index']}.html", "w") as f:
        f.write(i["answer_section_html"])
    print("-" * 100)
    print()
