# calculate_sov.py

def calculate_sov(mention_text, total_text):
  """Calculates the Share of Voice (SOV) based on text lengths.

  Args:
    mention_text: The string containing the text mentioning the organization.
    total_text: The string containing the full response text.

  Returns:
    The SOV percentage as a float, or 0.0 if total_text is empty.
  """
  mention_len = len(mention_text)
  total_len = len(total_text)

  if total_len == 0:
    return 0.0  # Avoid division by zero

  sov = (float(mention_len) / float(total_len)) * 100.0
  return sov

if __name__ == "__main__":
  print("--- SOV Calculator (Using Hardcoded Text) ---")

  # --- PASTE YOUR TEXTS HERE ---
  # Use triple quotes (""" """) for multi-line strings
  mention_text_input = """| Sun Life           | $24.58                    | $33.33                  |"""
  total_text_input = """For a healthy 35-year-old non-smoker in Ontario, Canada, a $500,000 20-year term life insurance policy typically costs between $21 and $33 per month, depending on the insurer and specific policy details. Here are some sample monthly premiums from various providers:\n\n| Insurance Provider | Monthly Premium for Women | Monthly Premium for Men |\n|--------------------|---------------------------|-------------------------|\n| PolicyMe           | $21.23                    | $28.97                  |\n| Industrial Alliance| $22.08                    | $29.58                  |\n| Manulife           | $23.48                    | $30.25                  |\n| BMO                | $24.16                    | $30.00                  |\n| Sun Life           | $24.58                    | $33.33                  |\n\n([policyme.com](https://www.policyme.com/term-life-insurance/term-life-insurance-quotes?utm_source=openai))\n\nPlease note that these rates are approximate and can vary based on individual factors such as health status, lifestyle, and specific policy terms. It's advisable to obtain personalized quotes from multiple insurers to find the best rate for your situation. """

  if not total_text_input:
    print("\nError: Total response text cannot be empty.")
  elif not mention_text_input:
      print("\nCalculating SOV assuming 0 mention text...")
      sov_percentage = 0.0
      print("\n---")
      print(f"Mention Text Length: {len(mention_text_input)}")
      print(f"Total Text Length:   {len(total_text_input)}")
      print(f"Calculated SOV:      {sov_percentage:.2f}%")
      print("---")
  else:
    sov_percentage = calculate_sov(mention_text_input, total_text_input)
    print("\n---")
    print(f"Mention Text Length: {len(mention_text_input)}")
    print(f"Total Text Length:   {len(total_text_input)}")
    print(f"Calculated SOV:      {sov_percentage:.2f}%")
    print("---")