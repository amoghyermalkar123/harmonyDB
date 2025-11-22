#!/bin/bash

echo "Stopping containers..."
docker-compose down

echo "Removing old data..."
rm -rf data/ logs/

echo "Rebuilding Docker images (no cache)..."
docker-compose build --no-cache

echo "Starting cluster..."
docker-compose up -d

echo "Waiting for startup..."
sleep 5

echo ""
echo "Cluster is starting up. Check logs with:"
echo "  docker-compose logs -f"
echo ""
echo "Access points:"
echo "  Node 1: http://localhost:8081 (Debug: http://localhost:6061)"
echo "  Node 2: http://localhost:8082 (Debug: http://localhost:6062)"
echo "  Node 3: http://localhost:8083 (Debug: http://localhost:6063)"
echo "  Grafana: http://localhost:3000 (admin/admin)"
echo "  Prometheus: http://localhost:9090"
