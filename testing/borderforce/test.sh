#!/usr/bin/env bash

set -euo pipefail

# Create a temporary directory
TESTDIR="$(mktemp -d -p ./)"
echo "Using temp dir: $TESTDIR"

# Define a cleanup function
cleanup() {
    echo "Cleaning up..."
    rm -vrf "$TESTDIR"
}

# Register the cleanup function to run on exit (normal or error)
trap cleanup EXIT

# === CERT and KEY CREATION ===

# Generate private key
KEYFILE="$TESTDIR/server.key"
openssl genrsa -out "$KEYFILE" 2048
echo "Generated: $KEYFILE"

# Generate CA cert config
CNFFILE="$TESTDIR/server.cnf"
cat > "$CNFFILE" <<EOF
[ req ]
default_bits       = 2048
prompt             = no
default_md         = sha256
distinguished_name = dn

[ dn ]
C  = AU
ST = Western Australia
L  = Perth
O  = Plane.Watch
OU = Testing Department
CN = dev.plane.watch
EOF
echo "Generated: $CNFFILE"

# Generate cert signing request
CSRFILE="$TESTDIR/server.csr"
openssl req -new -key "$KEYFILE" -out "$CSRFILE" -config "$CNFFILE" -batch
echo "Generated: $CSRFILE"

# Generate cert
CRTFILE="$TESTDIR/server.crt"
openssl x509 -req -days 365 -in "$CSRFILE" -signkey "$KEYFILE" -out "$CRTFILE"
echo "Generated: $CRTFILE"

# === GENERATE DOCKER COMPOSE .env FILE ===

ENVFILE="$TESTDIR/docker-compose.env"
cat > "$ENVFILE" <<EOF
TESTDIR=$TESTDIR
EOF

# === BRING UP TEST ENVIRONMENT ===
cat "$ENVFILE"
docker compose --env-file "$ENVFILE" up --build -d

# === TESTS GO HERE ===
read -p "press enter when done"

# === BRING DOWN TEST ENVIRONMENT ===
docker compose --env-file "$ENVFILE" down
