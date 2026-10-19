{.used.}

import chronos/timer
import unittest2
import ../src/[backoff, reporter]

suite "reporter backoff":
  test "the delay grows with each failure":
    check backoffDelay(0, 1.0) == 1.seconds
    check backoffDelay(1, 1.0) == 2.seconds
    check backoffDelay(2, 1.0) == 4.seconds
    check backoffDelay(5, 1.0) == 32.seconds

  test "the delay never exceeds a minute":
    for attempt in 6 .. 40:
      check backoffDelay(attempt, 1.0) == 60.seconds
      check backoffDelay(attempt, 0.0) == 30.seconds

  test "jitter keeps the delay within half to full":
    for attempt in 0 .. 8:
      let full = backoffDelay(attempt, 1.0)
      let half = backoffDelay(attempt, 0.0)
      check half * 2 == full
      check backoffDelay(attempt, 0.5) > half
      check backoffDelay(attempt, 0.5) < full

  test "out of range inputs are clamped":
    check backoffDelay(-5, 1.0) == 1.seconds
    check backoffDelay(3, 7.0) == backoffDelay(3, 1.0)
    check backoffDelay(3, -1.0) == backoffDelay(3, 0.0)

  test "retry-after hints are held within one second and five minutes":
    check clampRetryAfter(0) == 1.seconds
    check clampRetryAfter(250) == 1.seconds
    check clampRetryAfter(7000) == 7.seconds
    check clampRetryAfter(9_000_000) == 300.seconds
    check clampRetryAfter(-5) == 1.seconds
    check clampRetryAfter(high(int64)) == 300.seconds
    check clampRetryAfter(low(int64)) == 1.seconds

suite "reporter reactions":
  test "each status maps to one reaction":
    check reactToBatch(200) == BatchReaction.Acked
    check reactToBatch(404) == BatchReaction.Reregister
    check reactToBatch(429) == BatchReaction.RetryAfter
    check reactToBatch(413) == BatchReaction.Split
    check reactToBatch(400) == BatchReaction.Drop
    check reactToBatch(422) == BatchReaction.Drop

  test "auth failures, server errors and redirects back off":
    for status in [401, 403, 500, 502, 503, 504, 301, 302, 409]:
      check reactToBatch(status) == BatchReaction.Backoff
