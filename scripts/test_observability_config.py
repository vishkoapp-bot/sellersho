import unittest
from pathlib import Path
class EdgeTelemetry(unittest.TestCase):
 def test_caddy_metrics_stay_private_and_bounded(self):
  text=Path('Caddyfile').read_text();self.assertIn('\n\tmetrics\n',text);self.assertNotIn('per_host',text);self.assertNotIn('metrics /metrics',text)
 def test_nginx_redacts_request_values(self):
  text=Path('nginx.conf').read_text();self.assertIn('listen 8081;',text);self.assertNotIn('listen 127.0.0.1:8081',text);self.assertIn('allow 127.0.0.1; allow 172.18.0.0/16; deny all;',text);fmt=text.split('log_format sellersho_telemetry',1)[1].split(';',1)[0]
  for raw in ['$request_uri','$args','$remote_addr','$http_authorization','$http_cookie','$http_referer','$http_user_agent']:self.assertNotIn(raw,fmt)
  self.assertIn('$telemetry_class',fmt);self.assertIn('$request_time',fmt);self.assertIn('access_log /dev/stdout sellersho_telemetry',text)
if __name__=='__main__':unittest.main()
