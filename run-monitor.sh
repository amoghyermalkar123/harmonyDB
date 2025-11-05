#!/bin/bash

# HarmonyDB Raft Log Monitor
# This script builds and runs the Bubble Tea TUI monitor

set -e

echo "🔍 HarmonyDB Raft Log Monitor"
echo "============================="

# Check if Consul is running
if ! command -v consul &> /dev/null; then
    echo "⚠️  Consul not found. Installing Consul is recommended for service discovery."
    echo "   You can install it with: brew install consul"
    echo "   Then run: consul agent -dev"
    echo ""
fi

# Build the monitor
echo "🔨 Building monitor..."
cd cmd/monitor
go mod tidy
go build -o ../../bin/monitor .
cd ../..

# Create bin directory if it doesn't exist
mkdir -p bin

echo "✅ Monitor built successfully!"
echo ""
echo "🚀 Starting Raft Log Monitor..."
echo "   - The monitor connects to an existing Raft node via gRPC"
echo "   - Use arrow keys to navigate the table"
echo "   - Press 'r' to refresh manually"
echo "   - Press 'q' to quit"
echo ""

# Get port parameter
PORT=${1:-8080}

echo "💡 Usage examples:"
echo "   ./run-monitor.sh        # Connect to default port 8080"
echo "   ./run-monitor.sh 8081   # Connect to port 8081"
echo "   ./bin/monitor -port=8082 # Connect to port 8082"
echo ""
echo "📡 Connecting to Raft node on port $PORT..."
echo ""
echo "💡 To see interesting data, run some clients in other terminals:"
echo "   ./test-clients.sh"
echo ""

# Run the monitor with the specified port
./bin/monitor -port=$PORT