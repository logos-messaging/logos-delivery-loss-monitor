version = "0.1.0"
author = "Logos"
description = "Message loss monitor client for Logos Delivery"
license = "MIT or Apache License 2.0"
srcDir = "src"
bin = @["lossmon_client"]

requires "nim == 2.2.6"

# logos-delivery v0.39.2
requires "https://github.com/logos-messaging/logos-delivery#fd07b72c370a6b39c69870c087cc0d928d485327"

# Pins from logos-delivery's nimble.lock; a fresh resolve picks newer ones.
requires "https://github.com/arnetheduck/nim-sqlite3-abi#9645cad7f8c6d27a322ef48a60722a3f88f44c27"
requires "https://github.com/nitely/nim-regex#4593305ed1e49731fc75af1dc572dd2559aad19c"
requires "https://github.com/status-im/nim-bearssl#b6c9a38964a574f6bc51f69efaf8fb2fedea905e"
requires "https://github.com/status-im/nim-confutils#cdbac612f0f3d2883e959807d790658854e73cf2"
requires "https://github.com/status-im/nim-faststreams#059bc2f52546ac14b583639c33372c14b1d46fa3"
requires "https://github.com/status-im/nim-http-utils#7d771ca337bd3bf563f6834569d2a06085ac21f1"
requires "https://github.com/status-im/nim-json-serialization#73c37b46591cf3d6a19d39ff0ed14b2e77433778"
requires "https://github.com/status-im/nim-leopard#2e8de41205ea44b6f33b5cad676bab215b36c987"
requires "https://github.com/status-im/nim-minilru#e2d64213ce196c9069b40f09b63677e98a044d5d"
requires "https://github.com/status-im/nim-nat-traversal#83a4010c72917fd1386880d376392069960e12fb"
requires "https://github.com/status-im/nim-serialization#b5473564e80a4908cf6c5a038e4c4a6ca7de2223"
requires "https://github.com/status-im/nim-snappy#a99d113197e81bf764a3b005b0ade3f9f3758069"
requires "https://github.com/status-im/nim-stew#c75502ceedb5ed2a37b7b5e4744fc71313682859"
requires "https://github.com/status-im/nim-taskpools#7d96007793fdb2d244d392eb7c5cfa4a695d4ecc"
requires "https://github.com/status-im/nim-unittest2#26f2ef3ae0ec72a2a75bfe557e02e88f6a31c189"
requires "https://github.com/status-im/nim-zlib#190246aa0bb6569781370964fa2faa474203d6dd"
