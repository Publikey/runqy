#!/bin/bash
# Simulate server startup with real output
echo "[QUEUEWORKER] Loaded: quickstart-oneshot.default (priority=5)"
echo "[QUEUEWORKER] Loaded: quickstart-longrunning.default (priority=5)"
echo ""
cat << 'EOF'
  ______   __  __   __   __   ______   __  __
 /\  == \ /\ \/\ \ /\ "-.\ \ /\  __ \ /\ \_\ \
 \ \  __< \ \ \_\ \\ \ \-.  \\ \ \/\_\\ \____ \
  \ \_\ \_\\ \_____\\ \_\\"\_\\ \___\_\\/\_____\
   \/_/ /_/ \/_____/ \/_/ \/_/ \/___/_/ \/_____/  v0.2.6

  Server: http://localhost:3000

  Database     PostgreSQL (runqy)
  Redis        localhost:6379 [connected]
  Queues       2
  Vaults       disabled
  Monitoring   /monitoring
  Metrics      /metrics
  Swagger      /swagger

  Docs: https://runqy.com/docs/
EOF
