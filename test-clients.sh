#!/bin/bash

# Test script for running multiple HarmonyDB clients
# This script helps you quickly start multiple client instances for testing

set -e

echo "HarmonyDB Client Test Script"
echo "=========================="

# Build the client
echo "Building client..."
go build -o bin/client ./cmd/client

# Function to start a client with specific parameters
start_client() {
    local client_id=$1
    local interval=$2
    local key_range=$3
    local verbose=$4

    echo "Starting client: $client_id (interval: $interval, keys: $key_range)"
    ./bin/client -client="$client_id" -interval="$interval" -keys="$key_range" -v="$verbose" &
}

# Create bin directory if it doesn't exist
mkdir -p bin

echo ""
echo "Available test scenarios:"
echo "1. Light load (3 clients, 5s interval)"
echo "2. Medium load (5 clients, 2s interval)"
echo "3. Heavy load (8 clients, 1s interval)"
echo "4. Custom (specify your own parameters)"
echo "5. Single client (interactive)"

read -p "Choose scenario (1-5): " scenario

case $scenario in
    1)
        echo "Starting light load test..."
        start_client "light_1" "5s" "100" "false"
        start_client "light_2" "5s" "100" "false"
        start_client "light_3" "5s" "100" "false"
        ;;
    2)
        echo "Starting medium load test..."
        start_client "med_1" "2s" "200" "false"
        start_client "med_2" "2s" "200" "false"
        start_client "med_3" "2s" "200" "false"
        start_client "med_4" "2s" "200" "false"
        start_client "med_5" "2s" "200" "false"
        ;;
    3)
        echo "Starting heavy load test..."
        for i in {1..8}; do
            start_client "heavy_$i" "1s" "500" "false"
        done
        ;;
    4)
        read -p "Number of clients: " num_clients
        read -p "Write interval (e.g., 2s): " interval
        read -p "Key range: " key_range
        read -p "Verbose output (true/false): " verbose

        echo "Starting $num_clients clients..."
        for i in $(seq 1 $num_clients); do
            start_client "custom_$i" "$interval" "$key_range" "$verbose"
        done
        ;;
    5)
        echo "Starting single interactive client..."
        ./bin/client -v=true
        exit 0
        ;;
    *)
        echo "Invalid choice. Exiting."
        exit 1
        ;;
esac

echo ""
echo "All clients started. Press Ctrl+C to stop all clients."
echo "Monitor the output to see distributed writes in action."

# Wait for Ctrl+C and then kill all background processes
trap 'echo ""; echo "Stopping all clients..."; kill $(jobs -p) 2>/dev/null; exit 0' INT

# Keep script running
wait