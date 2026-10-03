# RabbitMQ Message Queue Inspector (`tahr-rabbitmq`)

Lightweight RabbitMQ message broker manager and queue monitor.

## Features
- **Queue Overview**: Monitor queue lengths, ready vs. unacknowledged messages, and consumer counts.
- **Exchange & Binding Graph**: Visualize exchanges (direct, topic, fanout, headers) and their routing keys.
- **Message Inspection**: Peek at messages without requeuing or acknowledge/reject on demand.
- **Test Publisher**: Send test messages with routing keys and custom headers.
- **REST & AMQP Support**: Uses the RabbitMQ Management HTTP API for fast, zero-dependency telemetry.
