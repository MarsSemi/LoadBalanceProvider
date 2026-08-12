#!/bin/bash

set -u

cd "$(dirname "$0")" || exit 1

echo "=== LoadBalanceProvider 啟動 ==="
echo "工作目錄: $(pwd)"

ensure_agent_properties() {
  if [[ -f "agent.properties" ]]; then
    return 0
  fi
  if [[ ! -f "agent.sample.properties" ]]; then
    echo "缺少 agent.properties，且找不到 agent.sample.properties" >&2
    return 1
  fi
  cp "agent.sample.properties" "agent.properties"
  echo "已由 agent.sample.properties 建立 agent.properties"
}

read_json_port() {
  local key="$1"
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p" agent.properties | head -n 1
}

port_pids() {
  local port="$1"
  lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u
}

wait_port_free() {
  local port="$1"
  local deadline="$2"
  local now

  while true; do
    if [[ -z "$(port_pids "$port")" ]]; then
      return 0
    fi

    now="$(date +%s)"
    if (( now >= deadline )); then
      return 1
    fi

    sleep 0.3
  done
}

kill_port() {
  local port="$1"
  local pids
  local pid

  if [[ -z "$port" || "$port" == "0" ]]; then
    return 0
  fi

  pids="$(port_pids "$port")"
  if [[ -z "$pids" ]]; then
    echo "Port $port 未被佔用"
    return 0
  fi

  echo "Port $port 被佔用，準備停止 PID: $(echo "$pids" | tr '\n' ' ')"
  while IFS= read -r pid; do
    [[ -z "$pid" ]] && continue
    if [[ "$pid" == "$$" ]]; then
      continue
    fi
    kill -TERM "$pid" 2>/dev/null || true
  done <<< "$pids"

  if wait_port_free "$port" "$(( $(date +%s) + 5 ))"; then
    echo "Port $port 已釋放"
    return 0
  fi

  pids="$(port_pids "$port")"
  if [[ -n "$pids" ]]; then
    echo "Port $port 仍被佔用，改用 KILL: $(echo "$pids" | tr '\n' ' ')"
    while IFS= read -r pid; do
      [[ -z "$pid" ]] && continue
      if [[ "$pid" == "$$" ]]; then
        continue
      fi
      kill -KILL "$pid" 2>/dev/null || true
    done <<< "$pids"
  fi

  if wait_port_free "$port" "$(( $(date +%s) + 3 ))"; then
    echo "Port $port 已釋放"
    return 0
  fi

  echo "無法釋放 Port $port，請確認是否為 root-owned 程序或系統保護程序。" >&2
  return 1
}

ensure_agent_properties || exit 1

HTTP_PORT="${HTTP_PORT:-$(read_json_port "http_port")}"
HTTPS_PORT="${HTTPS_PORT:-$(read_json_port "https_port")}"

kill_port "$HTTP_PORT" || exit 1
kill_port "$HTTPS_PORT" || exit 1

go mod tidy
go run ./src/cmd/loadbalanceprovider
