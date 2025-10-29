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
  mention_text_input = """### 2. **Sun Life Financial** * **Product Variety**: Sun Life also offers a broad range of products, including term life, permanent life insurance, critical illness, and disability insurance. They are known for strong offerings in both personal and business insurance.
* **Flexibility**: Sun Life policies are very customizable with flexible coverage options. Their universal life insurance, for example, allows policyholders to adjust premiums and death benefits over time, adapting to their changing needs.
* **Notable Features**:  
   * Offers a "Buy More, Pay Later" program to increase coverage later without health re-assessment.  
   * Numerous riders and add-ons.  
   * Flexible premium payment structures. || The best choice for you will depend on your personal needs, life stage, and financial goals. If you value flexibility and customization, **Manulife**, **Sun Life**, and **Canada Life** are often the top contenders due to their wide range of options and flexibility."""

  total_text_input = """When evaluating Canadian life insurance companies based on product variety and flexibility, a few companies stand out for offering a wide range of options to suit different needs. Here's a list of the top life insurance providers in Canada that are generally recognized for their strong product offerings and flexibility:

### 1\. **Manulife** * **Product Variety**: Manulife is one of Canada’s largest life insurance companies and offers a wide range of life insurance products, including term life, whole life, universal life, and critical illness insurance.
* **Flexibility**: Manulife provides flexible policies with options like convertible term life insurance (allowing you to switch to permanent insurance later), as well as riders to customize coverage for things like accidental death, disability, or critical illness.
* **Notable Features**:  
   * Comprehensive online tools and resources for clients.  
   * A variety of riders and customization options.  
   * Offers coverage for all life stages, from young families to older adults.

### 2\. **Sun Life Financial** * **Product Variety**: Sun Life also offers a broad range of products, including term life, permanent life insurance, critical illness, and disability insurance. They are known for strong offerings in both personal and business insurance.
* **Flexibility**: Sun Life policies are very customizable with flexible coverage options. Their universal life insurance, for example, allows policyholders to adjust premiums and death benefits over time, adapting to their changing needs.
* **Notable Features**:  
   * Offers a "Buy More, Pay Later" program to increase coverage later without health re-assessment.  
   * Numerous riders and add-ons.  
   * Flexible premium payment structures.

### 3\. **Canada Life** * **Product Variety**: Canada Life (formerly Great-West Life) provides a broad portfolio of products, including term life, whole life, universal life, and critical illness insurance. They have both individual and group plans.
* **Flexibility**: Canada Life is known for its flexible products that can adapt to various life stages. They offer a variety of premium payment options and riders that can add flexibility to their policies.
* **Notable Features**:  
   * Flexibility in terms of premiums and coverage amount.  
   * A wide selection of riders and customizable options.  
   * Strong reputation in both individual and group life insurance markets.

### 4\. **Industrial Alliance (iA Financial Group)** * **Product Variety**: iA Financial Group offers a variety of life insurance policies, including term, whole life, and universal life. They also offer specialized products like critical illness and disability insurance.
* **Flexibility**: iA provides flexible options, especially with their universal life policies, which allow for flexible premiums and adjustable coverage. They also offer different payment options (e.g., monthly, annually, or lump sum).
* **Notable Features**:  
   * Flexible premium options with both traditional and investment-linked policies.  
   * Customizable riders, including options for critical illness and income protection.  
   * Innovative products, such as "living benefits" insurance.

### 5\. **RBC Insurance** * **Product Variety**: RBC Insurance offers a solid range of life insurance products, including term, whole, and universal life insurance. Their offerings are complemented by critical illness, disability, and travel insurance.
* **Flexibility**: RBC provides various options for customizing policies, including convertible term life and universal life insurance, which allows for adjustments as your needs evolve.
* **Notable Features**:  
   * Ability to convert term life insurance to permanent insurance without health reassessment.  
   * Competitive pricing and flexible coverage options.  
   * Strong customer service with a focus on digital tools for managing policies.

### 6\. **Empire Life** * **Product Variety**: Empire Life offers a variety of insurance options including term life, whole life, universal life, and critical illness. They also have solutions for both individuals and businesses.
* **Flexibility**: Their universal life policies are flexible in terms of premium payments and adjusting coverage. They also provide options to add riders like critical illness or accidental death coverage.
* **Notable Features**:  
   * Customizable universal life policies.  
   * High degree of flexibility in terms of premium payments.  
   * Offers unique living benefits options.

### 7\. **Desjardins Insurance** * **Product Variety**: Desjardins offers term life, whole life, and universal life insurance, as well as critical illness insurance. They provide both individual and family coverage options.
* **Flexibility**: Their life insurance policies are highly customizable, with options for policyholders to adjust coverage and premiums over time.
* **Notable Features**:  
   * Flexible term options with the ability to convert to permanent insurance.  
   * Group insurance offerings alongside individual coverage.  
   * A range of rider options to suit different needs.

### 8\. **BMO Insurance** * **Product Variety**: BMO offers a good range of products, including term life, whole life, and universal life insurance. They also provide critical illness and disability insurance.
* **Flexibility**: BMO offers flexible term life insurance policies, with the option to convert to permanent coverage later. Their universal life policies offer flexibility in premiums and investment components.
* **Notable Features**:  
   * Convertibility of term policies.  
   * Options for policyholders to choose investment-linked life insurance.  
   * Simple and user-friendly online application process.

### Key Considerations for Flexibility and Variety:

* **Term Life Insurance**: For those looking for short-term coverage at a lower cost, most providers offer a variety of term life insurance options. Many also allow you to convert to permanent coverage later, adding flexibility.
* **Whole Life Insurance**: Provides lifelong coverage with a cash value component. This type of insurance is less flexible in terms of premiums, but it builds up a cash value over time.
* **Universal Life Insurance**: This is the most flexible form of permanent insurance, with the ability to adjust premiums and death benefits. It can also include an investment component.
* **Critical Illness and Disability Riders**: Many companies offer the ability to add these riders to a basic life insurance policy, increasing the flexibility to cover additional risks.

### Final Thoughts:

The best choice for you will depend on your personal needs, life stage, and financial goals. If you value flexibility and customization, **Manulife**, **Sun Life**, and **Canada Life** are often the top contenders due to their wide range of options and flexibility. However, if you are looking for strong offerings in terms of affordability and innovation, **RBC Insurance** and **iA Financial Group** might also be great options.

Would you like more specific details on any of these companies or their products?"""
  # --- END OF TEXT INPUT ---


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