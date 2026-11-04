CREATE TABLE IF NOT EXISTS seller_campaigns (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    name VARCHAR(200) NOT NULL,
    campaign_type VARCHAR(20) NOT NULL CHECK (campaign_type IN ('widget','banner','link')),
    status VARCHAR(20) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','paused','archived')),
    product_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    configuration JSONB NOT NULL DEFAULT '{}'::jsonb,
    starts_at TIMESTAMPTZ,
    ends_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_seller_campaigns_store ON seller_campaigns(store_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_seller_widget_campaign ON seller_campaigns(store_id, campaign_type) WHERE campaign_type = 'widget';

CREATE TABLE IF NOT EXISTS seller_campaign_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    campaign_id UUID NOT NULL REFERENCES seller_campaigns(id) ON DELETE CASCADE,
    store_id VARCHAR(50) NOT NULL REFERENCES stores(id) ON DELETE CASCADE,
    event_type VARCHAR(20) NOT NULL CHECK (event_type IN ('impression','click')),
    ad_token VARCHAR(200),
    product_id VARCHAR(50),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_seller_campaign_events_campaign ON seller_campaign_events(campaign_id, event_type, occurred_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_seller_campaign_click_token ON seller_campaign_events(campaign_id, ad_token) WHERE event_type = 'click' AND ad_token IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_seller_campaign_event ON seller_campaign_events(campaign_id,event_type,ad_token) WHERE ad_token IS NOT NULL;