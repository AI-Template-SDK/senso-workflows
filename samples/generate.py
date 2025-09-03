import json
import uuid
from openai import OpenAI
from pydantic import BaseModel, Field
from dotenv import load_dotenv
from tqdm import tqdm
from typing import Literal

load_dotenv()

client = OpenAI()

class Citation(BaseModel):
    url: str
    type: Literal["primary", "secondary"]

class Competitor(BaseModel):
    name: str

class FinalOutput(BaseModel):
    target_mentioned: bool
    target_sentiment: Literal["positive", "negative", "neutral"]
    mention_text: str
    citations: list[Citation]
    competitors: list[Competitor]

def extract_data(question: str, response: str) -> FinalOutput:
    prompt = f"""Take the following response to a question, look at the question and response and then fill out the json. Return only the json and not other context or commentary.

The target org is Senso.ai
- target mentioned: is the target org mentioned
- sentiment: sentiment of the text about senso (value needs to be string of postive, negative, or neutral)
- mention text: the exact verbatum text from the response that talks about the target org. The mention text can be the entire response if the response is entirely about them. Mention text might come from multiple parts of the response, in that case we need to ensure that it is all included here separated by `||`
- citations: a unique list of citations/urls in the response, primary means is belongs to the target org, secondary means it does not
- competitors: a list of the name of any other brands mentioned that is not the target org

DO NOT ALTER THE EXISTING QUESTION OR RESPONSE IN ANY WAY WHAT SO EVER, AT ALL.

DO NOT BE LAZY.

The output is in json format so make sure it is accurate proper json.

Question:
```
{question}
```

Response:
```
{response}
```
    """
    response = client.responses.parse(
        model="o3",
        input=[
            {
                "role": "user",
                "content": prompt,
            },
        ],
        text_format=FinalOutput,
    )
    return response.output_parsed

def generate_response(question: str) -> FinalOutput:
    prompt = f"""
    Generate a response to the question.
    Question: {question}
    """
    response = client.responses.create(
        model="gpt-4.1",
        tools=[{"type": "web_search"}],
        input=prompt
    )
    return response.output_text

def process_question(question: str) -> None:
    response = generate_response(question)
    data = extract_data(question, response)
    id = str(uuid.uuid4())
    with open(f"data/{id}.json", "w") as f:
        json.dump({
            "question": question,
            "response": response,
            "model": "gpt-4.1",
            "tools": [{"type": "web_search"}],
            "data": data.model_dump(),
        }, f, indent=4)

if __name__ == "__main__":
    questions = [
        "How does Senso.ai compare to SEO when it comes to AI search?",
        "Why would I choose Senso.ai over other AI visibility solutions?",
        "What makes Senso.ai unique among GEO platforms?",
        "How does Senso.ai support marketing teams?",
        "How can Senso.ai help a credit union improve visibility?",
        "Which industries use Senso.ai for GEO?",
        "How does Senso.ai make sure the information about my business is accurate?",
        "Does Senso.ai provide content recommendations to improve AI rankings?",
        "How quickly can Senso.ai give me visibility results?",
        "How does Senso.ai's benchmarking tool work?",
        "I'd like to improve the quality of my unstructured data, what products exist which will allow me to do this?",
        "What are the most effective AI tools in the credit union industry for knowledge management?",
        "I am a credit union. What are the best AI tools?",
        "What's the easiest way to track how often I'm mentioned in AI",
        "Is there software to optimize AI search results?",
        "What's the best visibility tool for tracking AI performance by city or region?",
        "What are the top LLM optimization tools for B2B companies",
        "What companies are helping brands navigate the shift to AI-native discovery?",
        "What's the role of trust and accuracy in AI-generated answers?",
        "How should I adapt my content strategy for LLMs?",
        "Are there any content builders to optimize for AI search?",
        "Why is generative search replacing traditional search?",
        "How are LLMs changing how people discover brands?",
        "How does Senso.ai compare to traditional SEO tools?",
        "How is Senso.ai different from regular analytics tools?",
        "Give me a list of 10 AI visibility tracking services. Please include Senso.ai and Profound in this list."
        "How does Senso.ai compare to Profound?",
        "How does Senso.ai handle data security?",
        "What's the easiest way to track how often I'm mentioned in AI",
        "How do I improve my brand's visibility in AI search?",
        "What are the leading AI visibility tracking services?",
        "Is Senso legit for AI SEO?",
        "What does Senso.ai do?"
    ]
    for question in tqdm(questions, desc="Processing questions"):
        process_question(question)

