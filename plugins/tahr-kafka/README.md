# Apache Kafka Stream Inspector (`tahr-kafka`)

Zero-CGO Kafka broker and topic visualizer for event-driven architectures.

## Features
- **Cluster & Topic Browser**: List topics, partitions, replication factors, and retention configurations.
- **Message Stream Consumer**: Tail message streams with support for JSON, Avro, Protobuf, and plain-text payloads.
- **Consumer Group Lag**: Track commit offsets vs. log-end offsets across active consumer groups with real-time lag indicators.
- **Message Producer**: Send test events with custom partition keys, headers, and payload bodies.
- **Pure-Go Protocol Engine**: Native protocol parser connecting directly over TCP without requiring C-based librdkafka.
