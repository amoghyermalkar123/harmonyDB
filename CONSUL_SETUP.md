# Consul Setup for HarmonyDB Raft Cluster

## Prerequisites

### Install Consul

**macOS (Homebrew):**
```bash
brew install consul
```

**Linux (Ubuntu/Debian):**
```bash
wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
sudo apt update && sudo apt install consul
```

**Windows (Chocolatey):**
```bash
choco install consul
```

**Direct Download:**
Download from https://developer.hashicorp.com/consul/downloads

## Starting Consul

### Development Mode (Single Node)
```bash
consul agent -dev
```

This starts Consul with:
- **Web UI**: http://localhost:8500
- **API**: http://localhost:8500/v1/
- **Data**: In-memory (lost on restart)

### Production Mode (Multi Node)
```bash
# On first node (server)
consul agent -server -bootstrap-expect=1 -data-dir=/tmp/consul -node=server1 -ui -client=0.0.0.0

# On additional nodes (clients)
consul agent -data-dir=/tmp/consul -node=client1 -retry-join=<server-ip>
```

## Testing HarmonyDB with Consul

### 1. Start Consul
```bash
# Terminal 0: Start Consul
consul agent -dev
```

### 2. Start Raft Nodes
```bash
# Terminal 1: First Raft node
cd raft/cmd && go build -o raft-test && ./raft-test

# Terminal 2: Second Raft node
cd raft/cmd && ./raft-test

# Terminal 3: Third Raft node
cd raft/cmd && ./raft-test
```

### 3. Monitor via Consul UI
Visit http://localhost:8500/ui/dc1/services/harmonydb-raft

You'll see:
- ✅ Registered nodes with health checks
- 🏷️ Service tags and metadata
- 📊 Node health status
- 🔗 Service topology

## Consul Features Used

### Service Registration
- **Service Name**: `harmonydb-raft`
- **Health Checks**: TCP checks every 10s
- **Metadata**: Node ID, version info
- **Tags**: `raft`, `consensus`, `node-id-{id}`

### Service Discovery
- **Query**: Healthy services only
- **Filtering**: Exclude self from peer list
- **Connection**: Automatic gRPC client creation
- **Retry Logic**: 5-second timeout per connection

### Health Monitoring
- **TCP Check**: Verifies gRPC server is running
- **Interval**: 10 seconds
- **Timeout**: 3 seconds
- **Auto-Deregister**: After 30 seconds of failure

## Troubleshooting

### "Failed to connect to consul"
```bash
# Check if Consul is running
consul members

# Check Consul logs
consul agent -dev -log-level=DEBUG
```

### "No peers discovered"
```bash
# Check service registration
consul services

# Check specific service
consul catalog services -service=harmonydb-raft
```

### "Connection refused"
- Ensure Consul is running on default port 8500
- Check firewall settings
- Verify network connectivity

## Advanced Configuration

### Custom Consul Address
Set environment variable:
```bash
export CONSUL_HTTP_ADDR="http://consul-server:8500"
```

Or modify `ConsulAddress` in `consul_discovery.go`:
```go
const ConsulAddress = "your-consul-server:8500"
```

### Multi-Datacenter Setup
```bash
# Configure datacenter in service registration
Meta: map[string]string{
    "datacenter": "dc1",
    "region":     "us-west",
}
```

## Production Considerations

1. **Security**: Enable Consul ACLs and TLS
2. **High Availability**: Run Consul cluster (3-5 servers)
3. **Monitoring**: Set up Consul monitoring and alerting
4. **Backup**: Regular Consul snapshots
5. **Network**: Proper security groups and firewalls