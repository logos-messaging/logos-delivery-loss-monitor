## Absolute-time rate schedule: ticks fall on `start + k / rate` and never burst to catch up.

{.push raises: [].}

import chronos/timer

type RateSchedule* = object
  period: Duration
  next: Moment

func init*(T: type RateSchedule, start: Moment, ratePerSec: float): T =
  ## `ratePerSec` must be positive.
  RateSchedule(period: nanoseconds(int64(1_000_000_000.0 / ratePerSec)), next: start)

func nextAt*(s: RateSchedule): Moment =
  s.next

func period*(s: RateSchedule): Duration =
  s.period

func advance*(s: var RateSchedule, now: Moment): tuple[fire: bool, missed: int] =
  ## Call when woken. When `now` has reached the next tick it fires once, skipping
  ## the ticks that fell a whole period or more behind and reporting how many.
  if now < s.next:
    return (false, 0)
  let behind = int((now - s.next).nanoseconds div s.period.nanoseconds)
  s.next = s.next + s.period * (behind + 1)
  (true, behind)
