# Redis Key-Value Inspector (`redis-inspector`)

Fast, production-safe Redis instance inspector and key visualizer.

## Features
- **Production-Safe SCAN**: Uses iterative `SCAN` queries instead of blocking `KEYS *` to safeguard production instances.
- **Hierarchical Key Tree**: Automatically groups keys into folders based on configured delimiters (e.g. `user:100:profile`).
- **Comprehensive Type Support**: Inspect and edit `string`, `hash`, `list`, `set`, `zset`, and `stream` data structures.
- **TTL Monitoring**: Real-time remaining TTL countdown and warning badges for volatile keys.
- **Direct Terminal Integration**: Launch an embedded `redis-cli` session directly inside the IDE.
