DROP TRIGGER IF EXISTS seller_campaign_history_trigger ON seller_campaigns;
DROP FUNCTION IF EXISTS record_seller_campaign_history();
DROP TABLE IF EXISTS seller_campaign_history;