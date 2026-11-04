CREATE TABLE IF NOT EXISTS seller_campaign_history (
 id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
 campaign_id UUID NOT NULL REFERENCES seller_campaigns(id) ON DELETE CASCADE,
 action VARCHAR(30) NOT NULL,
 actor_user_id UUID REFERENCES users(id),
 snapshot JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS seller_campaign_history_campaign ON seller_campaign_history(campaign_id,created_at);
-- Preserve valid existing widgets without creating historical traffic.
INSERT INTO seller_campaigns(store_id,name,campaign_type,status,product_ids,configuration,ends_at)
SELECT id,LEFT(seller_deals_widget_config->>'title',200),'widget','active',seller_deals_widget_config->'productIds',seller_deals_widget_config,
 CASE WHEN seller_deals_widget_config->>'endsAt' ~ '^\d{4}-\d{2}-\d{2}T' THEN (seller_deals_widget_config->>'endsAt')::timestamptz ELSE NULL END
FROM stores WHERE owner_type='dropshipper' AND jsonb_typeof(seller_deals_widget_config->'productIds')='array' AND LENGTH(seller_deals_widget_config->>'title')>0
ON CONFLICT DO NOTHING;
CREATE OR REPLACE FUNCTION record_seller_campaign_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO seller_campaign_history(campaign_id,action,actor_user_id,snapshot)
 VALUES(NEW.id,CASE WHEN TG_OP='INSERT' THEN 'created' ELSE 'updated' END,
 NULLIF(current_setting('app.campaign_actor',true),'')::uuid,to_jsonb(NEW));
 RETURN NEW;
END; $$;
DROP TRIGGER IF EXISTS seller_campaign_history_trigger ON seller_campaigns;
CREATE TRIGGER seller_campaign_history_trigger AFTER INSERT OR UPDATE ON seller_campaigns FOR EACH ROW EXECUTE FUNCTION record_seller_campaign_history();