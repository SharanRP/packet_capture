# Antrea PacketCapture Controller

A Kubernetes DaemonSet controller that performs on-demand packet captures on Pods using `tcpdump`.

## Overview

This controller runs as a DaemonSet and watches Pods on each Node. When a Pod is annotated with `tcpdump.antrea.io: "<N>"`, the controller automatically starts a packet capture using tcpdump with rotating files (1MB each, max N files). When the annotation is removed, the capture stops and files are cleaned up.

## Quick Start

### Setup and Deploy

```bash
make build
make deploy-all
kubectl get pods -n kube-system -l app=packet-capture
```

## Usage with Make Commands

### Start Packet Capture
```bash
make start-capture
kubectl annotate pod <pod-name> -n <namespace> tcpdump.antrea.io="5" --overwrite
```

### Monitor Capture Progress
```bash
make verify-capture
make logs
CAPTURE_POD=$(kubectl get pods -n kube-system -l app=packet-capture -o jsonpath="{.items[0].metadata.name}")
kubectl exec -n kube-system $CAPTURE_POD -- ls -lh /capture-test-pod.pcap*
```

### Extract Pcap File
```bash
make extract-pcap
tcpdump -r test-pod.pcap | head -20
```

### Stop Packet Capture
```bash
make stop-capture
kubectl annotate pod <pod-name> -n <namespace> tcpdump.antrea.io-
```

## Available Make Commands

| Command | Description |
|---------|-------------|
| `make help` | Show all available commands |
| `make build` | Build Docker image and load to Kind cluster |
| `make build-image` | Build Docker image (requires Docker) |
| `make kind-load` | Load pre-built image to Kind cluster |
| `make deploy-all` | Deploy controller, RBAC, and test Pod |
| `make deploy-controller` | Deploy controller and RBAC only |
| `make deploy-test` | Deploy test Pod only |
| `make start-capture` | Annotate test pod to start packet capture |
| `make stop-capture` | Remove annotation to stop packet capture |
| `make verify-capture` | Check if capture is running and list files |
| `make logs` | Tail logs from the controller pod |
| `make extract-pcap` | Extract pcap file from cluster to local directory |
| `make clean` | Delete controller and test pod |
| `make clean-all` | Delete controller, RBAC, and test pod |

## Implementation Details

- **Controller Pattern**: Uses Kubernetes Informer API to watch Pods (node-filtered for efficiency)
- **PID Discovery**: Walks `/host/proc` cgroups to find target container process
- **Namespace Isolation**: Executes tcpdump inside target Pod's network namespace via nsenter
- **File Rotation**: Configurable rotating files (1MB each, max N files per annotation value)
- **Automatic Cleanup**: Deletes pcap files when annotation is removed

## Architecture

The DaemonSet Pod:
- Runs on every node with `hostPID: true` and `privileged: true`
- Mounts `/host/proc` for process discovery
- Has minimal RBAC permissions (get, list, watch on Pods)
- Captures traffic in target Pod's namespace (isolated, not node-wide)

