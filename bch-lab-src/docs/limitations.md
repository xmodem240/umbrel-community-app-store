# Practical limitations

## History grows over time

Peer observations are kept so delivery history remains available. The 500-block ranking window does not limit how much history is stored.

On the original Bitcoin Lab author's Bitcoin Core node with around 200 peers, this data grew by about 4 MB per day, or roughly 1.5 GB per year (measured in October 2026; expect similar on Bitcoin Cash, not yet measured). The Storage panel shows current usage and lets you clear measurements while keeping your manual selection.

## Running BCHN on another machine

On Umbrel, Bitcoin Cash Lab and BCHN share the same system clock.

If you connect Bitcoin Cash Lab to BCHN on another machine, keep both clocks synchronised. The dashboard warns when it detects a time difference that could interfere with First attribution.

## Addresses Bitcoin Cash Lab cannot dial

Tor, I2P and CJDNS peers are measured, but Bitcoin Cash Lab cannot add them as manual connections. The same applies to inbound IPv6 peers whose real address is hidden by Docker on Umbrel.

These peers remain part of the ranking while connected. See [Configuration](configuration.md#docker-and-umbrel-networking) for the networking settings.

---

[← Back to the README](../README.md)
