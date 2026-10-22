# mining-pools.json

Which pool mined a block is read off the block itself: the coinbase transaction carries
the reward address and a short text the miner writes. `src/lib/pool-id.js` matches
either against this list.

The upstream project used the Bitcoin-only list from
[bitcoin-data/mining-pools](https://github.com/bitcoin-data/mining-pools). Bitcoin and
Bitcoin Cash share the address format, so that list would mislabel Bitcoin Cash blocks.
**The shipped file is therefore empty**, and unknown miners show the coinbase text as
written by the miner.

## Supplying a list

Same format, two maps:

```json
{
  "payout_addresses": { "<address>": { "name": "Pool name", "link": "https://..." } },
  "coinbase_tags":    { "<text in coinbase>": { "name": "Pool name", "link": "https://..." } }
}
```

Point `MINING_POOLS_FILE` at it, or replace `src/data/mining-pools.json`. Only add
entries you have checked against real Bitcoin Cash blocks.
