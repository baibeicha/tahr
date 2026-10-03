# gRPC & Protobuf Tooling (`grpc-proto`)

Essential tooling for microservices and Protocol Buffers development in Tahr IDE.

## Features
- **Protobuf Language Support**: Syntax highlighting and indentation for `.proto` (proto3 and proto2).
- **Code Formatting & Generation**: Seamless integration with `buf format` and `buf generate`.
- **gRPC Server Reflection**: Connect to live gRPC endpoints (e.g. `localhost:50051`), inspect service descriptors and methods dynamically without precompiled proto files.
- **RPC Invocation**: Send test JSON payloads to remote RPC methods and inspect streaming responses directly within the editor.
