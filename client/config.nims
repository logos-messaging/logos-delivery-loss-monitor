import std/os

if defined(release):
  switch("nimcache", "nimcache/release/$projectName")
else:
  switch("nimcache", "nimcache/debug/$projectName")

# Portable CPU baseline, matching how logos-delivery builds its dependencies.
if defined(macosx):
  switch("passC", "-Wno-error=incompatible-function-pointer-types")
  if defined(amd64):
    switch("passC", "-march=haswell -mtune=generic")
    switch("passL", "-march=haswell -mtune=generic")
    switch("define", "LeopardCmakeFlags=-DCMAKE_BUILD_TYPE=Release -DENABLE_OPENMP=off -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCOMPILER_SUPPORTS_MARCH_NATIVE=FALSE -DCMAKE_CXX_FLAGS=-march=haswell")
  else:
    switch("define", "LeopardCmakeFlags=-DCMAKE_BUILD_TYPE=Release -DENABLE_OPENMP=off -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCOMPILER_SUPPORTS_MARCH_NATIVE=FALSE")
else:
  if defined(amd64):
    switch("passC", "-mssse3")
    switch("passL", "-mssse3")
    switch("define", "LeopardCmakeFlags=-DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCOMPILER_SUPPORTS_MARCH_NATIVE=FALSE -DCMAKE_CXX_FLAGS=-mssse3")
  else:
    switch("define", "LeopardCmakeFlags=-DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON -DCOMPILER_SUPPORTS_MARCH_NATIVE=FALSE")
switch("define", "disableMarchNative")

--threads:on
--opt:speed
--excessiveStackTrace:on
--define:metrics
--define:nimTypeNames
--define:nimOldCaseObjects
switch("warning", "CaseTransition:off")
switch("warning", "ObservableStores:off")
switch("warning", "LockLevel:off")

switch("path", thisDir() / "src")

# begin Nimble config (version 2)
--noNimblePath
when withDir(thisDir(), system.fileExists("nimble.paths")):
  include "nimble.paths"
# end Nimble config
