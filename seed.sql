-- seed.sql
-- This file is executed after database migrations are complete
-- Creates initial seed data for senso-workflows development

-- Set timezone for consistency
SET timezone = 'UTC';

-- Create variables for UUIDs to maintain relationships
-- These will be generated as actual UUIDs
DO $$
DECLARE
    industry_uuid UUID;
    partner_uuid UUID;
    network_uuid UUID;
    org_uuid UUID;
    content_uuid UUID;
    content_version_uuid UUID;
    geo_pool_uuid UUID;
    geo_question1_uuid UUID;
    geo_question2_uuid UUID;
    geo_question3_uuid UUID;
    geo_question4_uuid UUID;
    geo_question5_uuid UUID;
    org_website_uuid UUID;
    geo_model_uuid UUID;
    org_location_uuid UUID;
BEGIN
    -- Generate UUIDs for all entities
    industry_uuid := uuid_generate_v4();
    partner_uuid := uuid_generate_v4();
    network_uuid := uuid_generate_v4();
    org_uuid := uuid_generate_v4();
    content_uuid := uuid_generate_v4();
    content_version_uuid := uuid_generate_v4();
    geo_pool_uuid := uuid_generate_v4();
    geo_question1_uuid := uuid_generate_v4();
    geo_question2_uuid := uuid_generate_v4();
    geo_question3_uuid := uuid_generate_v4();
    geo_question4_uuid := uuid_generate_v4();
    geo_question5_uuid := uuid_generate_v4();
    org_website_uuid := uuid_generate_v4();
    geo_model_uuid := uuid_generate_v4();
    org_location_uuid := uuid_generate_v4();

    -- 1. Insert Industry: "Startups"
    INSERT INTO industries (industry_id, name, slug, created_at, updated_at)
    VALUES (industry_uuid, 'Startups', 'startups', NOW(), NOW());

    -- 2. Insert Partner: "Senso.ai"
    INSERT INTO partners (partner_id, name, slug, created_at, updated_at)
    VALUES (partner_uuid, 'Senso.ai', 'senso-ai', NOW(), NOW());

    -- 3. Insert Network: "Senso Default" (references partner)
    INSERT INTO networks (network_id, name, slug, partner_id, created_at, updated_at)
    VALUES (network_uuid, 'Senso Default', 'senso-default', partner_uuid, NOW(), NOW());

    -- 4. Insert Organization: "Senso.ai" (references network, industry, partner)
    INSERT INTO orgs (org_id, name, slug, network_id, industry_id, partner_id, created_at, updated_at)
    VALUES (org_uuid, 'Senso.ai', 'senso-ai', network_uuid, industry_uuid, partner_uuid, NOW(), NOW());

    -- 5. Insert Organization Website
    INSERT INTO org_websites (org_website_id, org_id, url, created_at, updated_at)
    VALUES (org_website_uuid, org_uuid, 'https://senso.ai', NOW(), NOW());

    -- 6. Insert Geo Model
    INSERT INTO geo_models (geo_model_id, org_id, name, created_at, updated_at)
    VALUES (geo_model_uuid, org_uuid, 'gpt-4.1', NOW(), NOW());

    -- 7. Insert Organization Location
    INSERT INTO org_locations (org_location_id, org_id, country_code, region_name, created_at, updated_at)
    VALUES (org_location_uuid, org_uuid, 'CA', 'Ontario', NOW(), NOW());

    -- 8. Insert Content entry (markdown about pickle making)
    INSERT INTO content (id, org_id, type, created_at, updated_at)
    VALUES (content_uuid, org_uuid, 'raw', NOW(), NOW());

    -- 9. Insert Content Version
    INSERT INTO content_versions (id, content_id, version_num, title, summary, created_at, updated_at)
    VALUES (content_version_uuid, content_uuid, 1, 'The Art of Pickle Making', 'A comprehensive guide to making delicious pickles at home', NOW(), NOW());

    -- 10. Insert Content Raw Version with pickle making markdown
    INSERT INTO content_raw_versions (content_version_id, raw_text, format)
    VALUES (content_version_uuid, 
    '# The Art of Pickle Making

## Introduction

Pickle making is an ancient art that transforms ordinary vegetables into tangy, flavorful delights. This time-honored preservation method has been used for centuries to extend the shelf life of fresh produce while creating unique and delicious flavors.

## Essential Ingredients

### The Basics
- **Fresh vegetables** (cucumbers, carrots, onions, etc.)
- **Salt** (preferably non-iodized)
- **Vinegar** (white or apple cider)
- **Water** (filtered is best)

### Flavor Enhancers
- **Spices**: dill, peppercorns, mustard seeds, coriander
- **Herbs**: fresh dill, bay leaves, garlic
- **Aromatics**: onions, ginger, chili peppers

## The Pickling Process

### Step 1: Prepare Your Vegetables
1. Wash and clean all vegetables thoroughly
2. Cut to desired size (spears, chips, or whole)
3. If using cucumbers, remove blossom end to prevent softening

### Step 2: Create the Brine
1. Combine water, vinegar, and salt in a 1:1:1 ratio
2. Heat the mixture until salt dissolves completely
3. Add your chosen spices and herbs
4. Allow to cool to room temperature

### Step 3: Pack and Process
1. Pack vegetables tightly into sterilized jars
2. Pour cooled brine over vegetables, leaving ¼ inch headspace
3. Remove air bubbles by tapping jar gently
4. Wipe jar rims clean and apply lids

### Step 4: Fermentation or Processing
- **Quick pickles**: Refrigerate immediately, ready in 24 hours
- **Fermented pickles**: Leave at room temperature for 3-7 days, then refrigerate
- **Canned pickles**: Process in boiling water bath for long-term storage

## Pro Tips for Perfect Pickles

1. **Use the freshest ingredients** - vegetables should be crisp and unblemished
2. **Maintain proper ratios** - too little salt or vinegar can lead to spoilage
3. **Keep vegetables submerged** - use a clean weight if necessary
4. **Store properly** - refrigerated pickles last 2-3 months
5. **Experiment with flavors** - try different spice combinations

## Common Pickle Varieties

### Classic Dill Pickles
The most popular variety, featuring fresh dill and garlic for that classic pickle flavor.

### Bread and Butter Pickles
Sweet and tangy pickles made with onions and a sugar-enhanced brine.

### Spicy Pickles
Add heat with jalapeños, red pepper flakes, or hot sauce.

### Gourmet Variations
- Asian-inspired with ginger and soy
- Mediterranean with olives and herbs
- Mexican with lime and chili

## Troubleshooting

- **Soft pickles**: Usually caused by old vegetables or too little salt
- **Cloudy brine**: Normal for fermented pickles, concerning for quick pickles
- **Off flavors**: Often due to contamination or improper ratios

## Conclusion

Pickle making is both an art and a science. With practice and experimentation, you''ll develop your own signature recipes that friends and family will love. Remember that the best pickles start with the best ingredients and proper technique.

Happy pickling!', 
    'markdown');

    -- 11. Update content table with current version reference
    UPDATE content SET current_version_id = content_version_uuid WHERE id = content_uuid;

    -- 12. Create GEO data structures
    
    -- Create default geo pool
    INSERT INTO geo_pools (geo_pool_id, org_id, name, description, created_at, updated_at)
    VALUES (geo_pool_uuid, org_uuid, 'default', 'Default pool for generative engine optimization questions', NOW(), NOW());

    -- Create GEO questions about generative engine optimization and brand visibility
    
    -- Question 1: Brand-specific visibility
    INSERT INTO geo_questions (geo_question_id, org_id, question_text, type, geo_pool_id, created_at, updated_at)
    VALUES (geo_question1_uuid, org_uuid, 'What are the best AI-powered tools for content marketing and brand visibility optimization?', 'brand-specific', geo_pool_uuid, NOW(), NOW());

    -- Question 2: Comparison with competitors
    INSERT INTO geo_questions (geo_question_id, org_id, question_text, type, geo_pool_id, created_at, updated_at)
    VALUES (geo_question2_uuid, org_uuid, 'How do leading AI companies like OpenAI, Anthropic, and others optimize their brand presence in LLM responses?', 'comparison', geo_pool_uuid, NOW(), NOW());

    -- Question 3: Topic-based optimization
    INSERT INTO geo_questions (geo_question_id, org_id, question_text, type, geo_pool_id, created_at, updated_at)
    VALUES (geo_question3_uuid, org_uuid, 'What strategies should startups use to improve their visibility when users ask AI models about business intelligence tools?', 'topic', geo_pool_uuid, NOW(), NOW());

    -- Question 4: Ranking and positioning
    INSERT INTO geo_questions (geo_question_id, org_id, question_text, type, geo_pool_id, created_at, updated_at)
    VALUES (geo_question4_uuid, org_uuid, 'Which companies are most frequently mentioned by ChatGPT and Claude when discussing data analytics platforms for startups?', 'rank', geo_pool_uuid, NOW(), NOW());

    -- Question 5: Brand-specific optimization
    INSERT INTO geo_questions (geo_question_id, org_id, question_text, type, geo_pool_id, created_at, updated_at)
    VALUES (geo_question5_uuid, org_uuid, 'How can AI companies ensure their brand appears prominently when users ask language models for recommendations on machine learning platforms?', 'brand-specific', geo_pool_uuid, NOW(), NOW());

    RAISE NOTICE 'Seed data created successfully:';
    RAISE NOTICE '- Industry: Startups (%)' , industry_uuid;
    RAISE NOTICE '- Partner: Senso.ai (%)' , partner_uuid;
    RAISE NOTICE '- Network: Senso Default (%)' , network_uuid;
    RAISE NOTICE '- Organization: Senso.ai (%)' , org_uuid;
    RAISE NOTICE '- Org Website: https://senso.ai (%)' , org_website_uuid;
    RAISE NOTICE '- Geo Model: gpt-4.1 (%)' , geo_model_uuid;
    RAISE NOTICE '- Org Location: Ontario, CA (%)' , org_location_uuid;
    RAISE NOTICE '- Content: The Art of Pickle Making (%)' , content_uuid;
    RAISE NOTICE '- Geo Pool: default (%)' , geo_pool_uuid;
    RAISE NOTICE '- Created 5 GEO questions about brand visibility and LLM optimization';

END $$; 