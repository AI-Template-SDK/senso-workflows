import re
import json

with open("q8.json", "r") as f:
    data = json.load(f)
    data = data["response"]

# Fixed regex that properly handles subdomains and periods in paths, excludes trailing punctuation
regex = re.compile(r'https?://(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}(?:/[^\s,)}\]]*[^\s,.)}\]])?|www\.(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}(?:/[^\s,)}\]]*[^\s,.)}\]])?')

matches = re.findall(regex, data)
unique_matches = list(set(matches))

for m in unique_matches:
    print(m)