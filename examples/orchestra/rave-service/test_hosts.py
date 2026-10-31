"""The names the services answer (mcp_http.Hosts): the same file in both."""

import unittest

import mcp_http


class HostsTest(unittest.TestCase):
    def setUp(self):
        self.hosts = mcp_http.Hosts(public=["Orquesta.underpassai.com"], internal=["score", "score.orchestra.svc:8790"])

    def test_kinds(self):
        for header, kind in [("127.0.0.1:8790", "loopback"), ("localhost", "loopback"), ("[::1]:8790", "loopback"),
                             ("score:8790", "internal"), ("score.orchestra.svc", "internal"),
                             ("orquesta.underpassai.com", "public"), ("ORQUESTA.underpassai.com:443", "public"),
                             ("evil.example", None), ("", None), (None, None)]:
            self.assertEqual(self.hosts.kind(header), kind, header)

    def test_loopback_only_by_default(self):
        hosts = mcp_http.Hosts()
        self.assertFalse(hosts.beyond_loopback())
        self.assertEqual(hosts.kind("127.0.0.1:8790"), "loopback")
        self.assertIsNone(hosts.kind("score:8790"))
        self.assertTrue(self.hosts.beyond_loopback())

    def test_origins(self):
        self.assertTrue(self.hosts.origin_ok(None))
        self.assertTrue(self.hosts.origin_ok("http://localhost:8790"))
        self.assertTrue(self.hosts.origin_ok("https://orquesta.underpassai.com"))
        self.assertFalse(self.hosts.origin_ok("https://evil.example"))
        # An internal name is for pods, not for a page.
        self.assertFalse(self.hosts.origin_ok("http://score:8790"))

    def test_a_public_name_is_never_internal(self):
        hosts = mcp_http.Hosts(public=["a.example"], internal=["a.example"])
        self.assertEqual(hosts.kind("a.example"), "public")


if __name__ == "__main__":
    unittest.main()
