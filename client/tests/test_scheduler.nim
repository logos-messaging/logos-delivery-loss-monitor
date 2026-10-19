{.used.}

import chronos/timer
import unittest2
import ../src/scheduler

let t0 = Moment.now()

suite "rate schedule":
  test "ticks fall on a fixed grid":
    var s = RateSchedule.init(t0, 2.0)
    check s.period == 500.milliseconds
    check s.advance(t0) == (true, 0)
    check s.nextAt() == t0 + 500.milliseconds
    check s.advance(t0 + 500.milliseconds) == (true, 0)
    check s.nextAt() == t0 + 1000.milliseconds

  test "an early wake does not fire":
    var s = RateSchedule.init(t0, 1.0)
    discard s.advance(t0)
    check s.advance(t0 + 400.milliseconds) == (false, 0)
    check s.nextAt() == t0 + 1.seconds

  test "a slightly late wake fires once and keeps the grid":
    var s = RateSchedule.init(t0, 1.0)
    discard s.advance(t0)
    check s.advance(t0 + 1300.milliseconds) == (true, 0)
    check s.nextAt() == t0 + 2.seconds

  test "falling behind skips whole periods instead of bursting":
    var s = RateSchedule.init(t0, 1.0)
    discard s.advance(t0)
    let tick = s.advance(t0 + 5500.milliseconds)
    check tick.fire
    check tick.missed == 4
    check s.nextAt() == t0 + 6.seconds

  test "after a long stall only one send is due":
    var s = RateSchedule.init(t0, 0.5)
    discard s.advance(t0)
    var fires = 0
    discard s.advance(t0 + 1.hours)
    for step in 1 .. 3:
      if s.advance(t0 + 1.hours + step.milliseconds).fire:
        inc fires
    check fires == 0

  test "a phase offset delays the first tick":
    var s = RateSchedule.init(t0 + 700.milliseconds, 1.0)
    check s.advance(t0) == (false, 0)
    check s.advance(t0 + 700.milliseconds) == (true, 0)

  test "a low rate gives a long period":
    let s = RateSchedule.init(t0, 0.5)
    check s.period == 2.seconds
