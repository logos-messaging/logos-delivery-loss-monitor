## Retry timing for the reporter.

{.push raises: [].}

import chronos/timer

const
  BackoffMin* = 1.seconds
  BackoffMax* = 60.seconds
  RetryAfterMin* = 1.seconds
  RetryAfterMax* = 300.seconds

func backoffDelay*(attempt: int, jitter: float): Duration =
  ## Exponential from 1 s to 60 s. `jitter` in [0, 1] scales the delay between 50% and 100%.
  let exp = min(max(attempt, 0), 6)
  let base = min(BackoffMin * (1 shl exp), BackoffMax)
  let factor = 0.5 + 0.5 * min(max(jitter, 0.0), 1.0)
  nanoseconds(int64(float(base.nanoseconds) * factor))

func clampRetryAfter*(hintMs: int64): Duration =
  ## A server retry hint, held within [1 s, 300 s].
  milliseconds(min(max(hintMs, RetryAfterMin.milliseconds), RetryAfterMax.milliseconds))
