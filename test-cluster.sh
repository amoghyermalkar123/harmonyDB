#!/bin/bash

# Test script for HarmonyDB Raft cluster with Consul Service Discovery
# Run this in 4 different terminals

echo "=== HarmonyDB Raft Cluster Test (Consul Service Discovery) ==="
echo ""
echo "🚀 SETUP STEPS:"
echo "1. Terminal 0: Start Consul server"
echo "   consul agent -dev"
echo ""
echo "2. Terminal 1-3: Start Raft nodes"
echo "   cd raft/cmd && go build -o raft-test && ./raft-test"
echo ""
echo "3. Monitor via Consul UI:"
echo "   http://localhost:8500/ui/dc1/services/harmonydb-raft"
echo ""
echo "🔧 FEATURES:"
echo "✅ Production-ready Consul service discovery"
echo "✅ Automatic peer registration and health checks"
echo "✅ Real-time cluster membership updates"
echo "✅ Graceful node shutdown and deregistration"
echo "✅ Leader election and log replication visualization"
echo ""
echo "📊 EXPECTED BEHAVIOR:"
echo "- Nodes auto-register with Consul on startup"
echo "- One node becomes leader (watch election process)"
echo "- Leader replicates random data every 5 seconds"
echo "- Only leader accepts Put requests"
echo "- Followers reject Put requests with error messages"
echo "- Graceful shutdown with Ctrl+C deregisters from Consul"
echo ""
echo "🐛 TROUBLESHOOTING:"
echo "- If 'Failed to connect to consul': Run 'consul agent -dev' first"
echo "- Check Consul UI for service health status"
echo "- See CONSUL_SETUP.md for detailed instructions"
echo ""

# Check if Consul is running
if ! curl -s http://localhost:8500/v1/status/leader > /dev/null 2>&1; then
    echo "❌ Consul is not running!"
    echo "💡 Start Consul first: consul agent -dev"
    exit 1
fi

echo "✅ Consul is running, starting Raft node..."
cd raft/cmd

# Build if needed
if [ ! -f "./raft-test" ] || [ "main.go" -nt "./raft-test" ]; then
    echo "🔨 Building raft-test..."
    go build -o raft-test main.go
fi

./raft-test