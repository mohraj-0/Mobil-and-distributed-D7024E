#!/bin/sh
set -eu

# A separate interactive node joins the running stack; it does not replace the
# container's existing server on port 8000. OWNERS_FILE is inherited from it.
node_ip=$(hostname -i | awk '{print $1}')
cli_port=${CLI_PORT:-9000}
exec /app/kadlab \
  --transport udp \
  --listen-addr "0.0.0.0:$cli_port" \
  --addr "$node_ip:$cli_port" \
  --bootstrap-addr "${BOOTSTRAP_ADDR:-kademlia-bootstrap:8000}" \
  "$@"
