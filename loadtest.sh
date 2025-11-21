#!/bin/bash

# HarmonyDB Load Tester

# Get port from user if not provided as argument
if [ -z "$1" ]; then
    read -p "Enter node port (default 8081): " NODE_PORT
    NODE_PORT=${NODE_PORT:-8081}
else
    NODE_PORT=$1
fi

# Get RPS from user if not provided as argument
if [ -z "$2" ]; then
    read -p "Enter requests per second (default 10): " RPS
    RPS=${RPS:-10}
else
    RPS=$2
fi

# Get duration from user if not provided as argument
if [ -z "$3" ]; then
    read -p "Enter duration in seconds (default 60): " DURATION
    DURATION=${DURATION:-60}
else
    DURATION=$3
fi

BASE_URL="http://localhost:${NODE_PORT}"
DELAY=$(echo "scale=4; 1/$RPS" | bc)
END_TIME=$((SECONDS + DURATION))

echo "Load Test Configuration:"
echo "  Target: $BASE_URL"
echo "  Rate: $RPS requests/second"
echo "  Duration: $DURATION seconds"
echo "  Delay between requests: ${DELAY}s"
echo ""
echo "Starting load test..."
echo ""

REQUEST_COUNT=0
SUCCESS_COUNT=0
FAILURE_COUNT=0

while [ $SECONDS -lt $END_TIME ]; do
    REQUEST_COUNT=$((REQUEST_COUNT + 1))
    KEY="key_${REQUEST_COUNT}_$(date +%s%N)"
    VALUE="value_${REQUEST_COUNT}_$(openssl rand -hex 8)"

    # PUT request
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST "${BASE_URL}/put" \
        -H "Content-Type: application/json" \
        -d "{\"key\": \"${KEY}\", \"value\": \"${VALUE}\"}" \
        --connect-timeout 2 \
        --max-time 5)

    if [ "$HTTP_CODE" = "200" ]; then
        SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
        echo -ne "\rRequests: $REQUEST_COUNT | Success: $SUCCESS_COUNT | Failed: $FAILURE_COUNT"
    else
        FAILURE_COUNT=$((FAILURE_COUNT + 1))
        echo -ne "\rRequests: $REQUEST_COUNT | Success: $SUCCESS_COUNT | Failed: $FAILURE_COUNT (HTTP $HTTP_CODE)"
    fi

    sleep $DELAY
done

echo ""
echo ""
echo "Load Test Complete!"
echo "===================="
echo "Total Requests: $REQUEST_COUNT"
echo "Successful: $SUCCESS_COUNT"
echo "Failed: $FAILURE_COUNT"
echo "Success Rate: $(echo "scale=2; $SUCCESS_COUNT * 100 / $REQUEST_COUNT" | bc)%"
