#!/usr/bin/env bash
# Mines one regtest block via the dev BCHN node, so you can watch
# relay-profiler pick it up over ZMQ in real time:
#   docker compose -f docker-compose.dev.yml logs -f relay-profiler
# UNTESTED against BCHN: assumes bitcoin-cli is on the image and that the node
# has a wallet (getnewaddress). Adjust if your image differs.
set -euo pipefail

BCLI="docker compose -f docker-compose.dev.yml exec -T bitcoind bitcoin-cli -regtest -rpcport=18443 -rpcuser=bchlab -rpcpassword=bchlab"

ADDR=$($BCLI getnewaddress)
$BCLI generatetoaddress 1 "$ADDR"
