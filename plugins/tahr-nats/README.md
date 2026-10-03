# NATS & JetStream Inspector (`tahr-nats`)

High-speed messaging inspector for NATS Core and JetStream persistence engine.

## Features
- **NATS Core Pub/Sub**: Real-time subject monitor, wildcard topic matcher (`foo.*.bar`, `orders.>`), and test message publisher.
- **JetStream Architecture**: View streams, storage limits (file/memory), consumer states, sequence numbers, and acknowledged messages.
- **KeyValue (KV) & Object Store**: Browse NATS KV buckets and read revision-controlled values.
- **Low-Latency Wire Protocol**: Native pure Go TCP client implementing NATS wire protocol with zero external C dependencies.
- **Metrics & Telemetry**: Monitor server statistics (connections, bytes in/out, slow consumers).
