# Remote SSH Development (`remote-ssh`)

Zero-latency remote development over SSH tunnels with Tahr IDE Headless Core.

## Features
- **Headless Core Architecture**: Automatically deploys or connects to a lightweight, pure Go headless `tahr-server` on the remote host.
- **SSH Config Discovery**: Automatically parses `~/.ssh/config` to list known hosts, identity files, and jump proxies.
- **Port Forwarding**: Forward remote development ports (e.g. `8080`, `3000`, `5432`) to localhost with one click.
- **Integrated Remote Terminal**: Open native remote shell sessions seamlessly inside Tahr IDE.
- **Secure File Synchronization**: Low-overhead diff-based synchronization and remote buffer editing without mounting latency.
