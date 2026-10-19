{.used.}

import results, unittest2
import ../src/conf

suite "preset allowlist":
  test "logos.dev and logos.test are allowed":
    check checkPreset("logos.dev", "").get() == "logos.dev"
    check checkPreset("logos.test", "").get() == "logos.test"

  test "preset names are canonicalised":
    check checkPreset("LogosDev", "").get() == "logos.dev"
    check checkPreset("logostest", "").get() == "logos.test"

  test "any other preset needs an explicit allow":
    check checkPreset("status.prod", "").isErr()
    check checkPreset("status.prod", "logos.dev").isErr()
    check checkPreset("statusprod", "status.prod").get() == "status.prod"

  test "twn is always refused":
    check checkPreset("twn", "").isErr()
    check checkPreset("twn", "twn").isErr()

  test "unknown presets are refused unless allowed":
    check checkPreset("nonsense", "").isErr()

suite "server url policy":
  test "https is accepted":
    check checkServerUrl("https://lossmon.example.org", false).isOk()

  test "http to loopback is accepted":
    check checkServerUrl("http://127.0.0.1:8080", false).isOk()
    check checkServerUrl("http://localhost:8080", false).isOk()
    check checkServerUrl("http://[::1]:8080", false).isOk()

  test "http to any other host needs the insecure flag":
    check checkServerUrl("http://server:8080", false).isErr()
    check checkServerUrl("http://10.0.0.5:8080", false).isErr()
    check checkServerUrl("http://server:8080", true).isOk()

  test "other schemes and malformed urls are refused":
    check checkServerUrl("ftp://host", true).isErr()
    check checkServerUrl("host:8080", true).isErr()
    check checkServerUrl("", true).isErr()
    check checkServerUrl("http://", true).isErr()

  test "credentials in the url are refused":
    check checkServerUrl("https://user:secret@host", false).isErr()
    check checkServerUrl("https://user@host", false).isErr()

  test "the normalised url is returned":
    check checkServerUrl("https://host:8443", false).get() == "https://host:8443"
