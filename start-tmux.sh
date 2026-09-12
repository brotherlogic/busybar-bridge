#!/bin/bash

# Ensure the 'prod' session exists
if ! tmux has-session -t busybar-bridge 2>/dev/null; then
  # Create a new session named 'prod', detached
  cd /workspaces/busybar-bridge
  tmux new-session -d -s busybar-bridge
fi
