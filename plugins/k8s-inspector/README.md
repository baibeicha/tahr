# Kubernetes Cluster Inspector (`k8s-inspector`)

Lightweight Kubernetes cluster navigator and pod troubleshooter for Tahr IDE.

## Features
- **Context & Namespace Selector**: Instantly switch clusters and namespaces from `~/.kube/config`.
- **Resource Explorer**: Tree navigation of Pods, Deployments, StatefulSets, Services, ConfigMaps, and Secrets.
- **Streaming Pod Logs**: Real-time log aggregation across container replicas with filtering.
- **Interactive Container Exec**: Launch interactive bash/sh terminals directly inside cluster pods.
- **Pure-Go Kubernetes REST Client**: Communicates directly with the Kubernetes API server over HTTPS/mTLS.
