#!/usr/bin/env bash
# Universal API Platform — cURL Integration Examples

set -euo pipefail

BASE_URL="${UAP_BASE_URL:-http://localhost:8080}"
: "${UAP_API_KEY:?Set UAP_API_KEY to a provisioned client key}"
API_KEY="$UAP_API_KEY"

echo "=== 1. Health Probe ==="
curl -s -i "${BASE_URL}/health"
echo -e "\n"

echo "=== 2. Current Weather (London) ==="
curl -s -i -H "X-API-Key: ${API_KEY}" \
  -H "traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" \
  "${BASE_URL}/v1/weather?location=London"
echo -e "\n"

echo "=== 3. Forward Geocoding (Paris, France) ==="
curl -s -i -H "Authorization: Bearer ${API_KEY}" \
  "${BASE_URL}/v1/geocode?q=Paris,+France&limit=1"
echo -e "\n"

echo "=== 4. Reverse Geocoding (Coordinates) ==="
curl -s -i -H "X-API-Key: ${API_KEY}" \
  "${BASE_URL}/v1/geocode?lat=48.85837&lon=2.29448"
echo -e "\n"

echo "=== 5. Scrape Prometheus Metrics ==="
curl -s "${BASE_URL}/metrics" | head -n 25
echo -e "\n"
