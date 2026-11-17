import unittest
from capacity_browser_relay import validate_target, BrowserRelay

class RelayGuardTests(unittest.TestCase):
    def test_refuses_arbitrary_or_incomplete_targets(self):
        nonce='a'*24
        target=dict(nonce=nonce,network='sellersho-cap-'+nonce,network_id='b'*64,tls_cid='c'*64,relay_cid='d'*64)
        validate_target(target)
        for key,value in [('nonce',''),('network','bridge'),('tls_cid','localhost'),('host','example.com')]:
            with self.subTest(key=key),self.assertRaises(ValueError):validate_target(dict(target,**{key:value}))
    def test_binds_only_ephemeral_loopback_and_closes_without_children(self):
        nonce='a'*24;calls=[]
        relay=BrowserRelay(dict(nonce=nonce,network='sellersho-cap-'+nonce,network_id='b'*64,tls_cid='c'*64,relay_cid='d'*64),lambda:calls.append(True))
        try:
            self.assertEqual(relay.server.server_address[0],'127.0.0.1')
            self.assertGreater(relay.port,0)
            self.assertEqual(calls,[True])
        finally:relay.close()
        self.assertEqual(relay.server.socket.fileno(),-1)
        self.assertEqual(relay.active,{})

if __name__=='__main__':unittest.main()
