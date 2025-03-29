# Key-Value Cache Server

A high-performance, in-memory key-value cache server implemented in Go, optimized for high throughput and low latency. The server implements a sharded LRU cache with RESP protocol support.

## Features

- Sharded LRU cache architecture for concurrent access
- RESP (Redis Serialization Protocol) compatible
- TCP connection optimization with NoDelay and custom buffer sizes
- Memory-efficient design with configurable limits
- Zero allocation optimization for hot paths
- Lock-free reads where possible

## Performance Optimizations

### Cache Design

- **Sharding**: Uses 64 independent LRU caches to reduce lock contention
- **Pre-allocated Maps**: Each shard pre-allocates its hash map to reduce resize operations
- **Fine-grained Locking**: Separate read/write locks per shard for maximum concurrency

### Memory Management

- **Controlled GC**: Custom GOGC settings to balance throughput and memory
- **Buffer Pooling**: Reuses buffers to reduce GC pressure
- **Entry Limits**: Hard limits on key/value sizes and entries per shard
- **Zero Allocation**: Critical paths optimized to minimize allocations

### Network Optimizations

- **TCP NoDelay**: Disabled Nagle's algorithm for lower latency
- **Custom Buffer Sizes**: Optimized read/write buffer sizes
- **Connection Pooling**: Client-side connection pooling in load tester
- **Minimized Syscalls**: Batched reads and writes where possible

## Getting Started

### Prerequisites

- Docker
- Go (for local builds)
- Locust (for load testing)

### Option 1: Run from Docker Hub

The easiest way to get started is to pull and run the pre-built Docker image:

```bash
# Pull the latest image
docker pull bugfinderr/server

# Run with optimized settings
docker run --cpus="2" --memory="2g" \
  --ulimit nofile=65536:65536 \
  --sysctl net.core.somaxconn=65535 \
  --sysctl net.ipv4.tcp_max_syn_backlog=65535 \
  -p 7171:7171 bugfinderr/server
```

### Option 2: Clone and Build Locally

```bash
# Clone the repository
git clone https://github.com/Bug-Finderr/hld-key-value-cache.git
cd redis

# Build locally (requires Go 1.24.2+)
go build -o server

# Run locally
./server
```

## Configuration

Key configuration constants in main.go:

```go
const (
    numShards               = 16      // Number of cache shards
    maxCacheEntriesPerShard = 100_000 // Max entries per shard
    port                    = 7171    // Server port
    readBufferSize          = 65536   // TCP read buffer size
    maxKeyValueSize         = 256     // Max key/value length
)
```

## Load Testing with Locust

### 1. Set up a Virtual Environment (Recommended)

It's recommended to set up a virtual environment to isolate Locust and its dependencies.

```bash
# Create a virtual environment
python3 -m venv venv

# Activate the virtual environment
# On Unix or macOS
source venv/bin/activate

# On Windows
venv\Scripts\activate
```

### 2. Install Locust

```bash
pip install locust
```

### 3. Configure System Limits (macOS)

```bash
sudo sysctl -w kern.maxfiles=2048000
sudo sysctl -w kern.maxfilesperproc=1800000
sudo sysctl -w kern.ipc.maxsockbuf=8388608
sudo sysctl -w net.inet.tcp.sendspace=262144
sudo sysctl -w net.inet.tcp.recvspace=262144
sudo sysctl -w net.inet.tcp.msl=1000
sudo sysctl -w kern.ipc.somaxconn=4096
ulimit -n 1800000
```

### 4. Run Locust

```bash
locust --host tcp://<server_ip_address>:7171 --users 10000 --spawn-rate 100 --run-time 180s --processes -1
```

Replace `<server_ip_address>` with the actual IP address of your server.

### 5. Accessing the Locust Web UI

Once Locust is running, access the web UI in your browser at:

```
http://localhost:8089
```

If running Locust remotely, replace `localhost` with the IP address of the machine running Locust.

### 6. Finding the Server IP Address

#### Local Machine

If running the server locally, the IP address is usually `127.0.0.1` or `localhost`.

#### Docker Container

1. **Inspect the container:**

    ```bash
    docker inspect <container_id> | grep IPAddress
    ```

    Replace `<container_id>` with the actual container ID.

2. **Get the IP address:**

    The output will include the IP address of the container.

#### Remote Server (e.g., AWS)

1. **AWS Console:**

    - Go to the EC2 Management Console.
    - Find the instance running the server.
    - The public IP address is listed in the instance details.

2. **Command Line (AWS CLI):**

    ```bash
    aws ec2 describe-instances --instance-ids <instance_id> --query 'Reservations[*].Instances[*].PublicIpAddress' --output text
    ```

    Replace `<instance_id>` with the actual instance ID.

### 7. Locust Configuration

- **Number of Users:** 10000
- **Spawn Rate:** 100 users/second
- **Run Time:** 180 seconds

After starting the load test, Locust will simulate 10000 users making requests to your server.

## Protocol

The server implements a subset of the RESP protocol:

### PUT Command

```
*3\r\n$3\r\nPUT\r\n$3\r\nkey\r\n$5\r\nvalue\r\n
```

### GET Command

```
*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n
```

## Monitoring & Debugging

To monitor server performance:

```bash
# Monitor network connections
netstat -an | grep 7171

# Check memory usage
docker stats kv-cache
```

## License

[MIT License](LICENCE)
