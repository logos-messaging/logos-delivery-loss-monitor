## Process shutdown request, safe to set from a signal handler.

{.push raises: [].}

import std/atomics

var
  shutdownSignal: Atomic[int]
  exitCodeValue: Atomic[int]

proc requestShutdown*(signal: cint) {.noconv.} =
  shutdownSignal.store(int(signal))

proc requestExit*(code: int) =
  ## Stops the client and makes the process exit with `code`.
  exitCodeValue.store(code)
  shutdownSignal.store(1)

proc isRequested*(): bool =
  shutdownSignal.load() != 0

proc exitCode*(): int =
  exitCodeValue.load()
