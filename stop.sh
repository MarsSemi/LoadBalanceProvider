#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

APP_NAME="LoadBalanceProvider"
PID_FILE="run.pid"
ROOT_DIR="$(pwd -P)"
SYSTEMD_UNIT="${LBP_SYSTEMD_UNIT:-load-balance-provider.service}"

ensure_agent_properties() {
  if [[ -f "agent.properties" ]]; then
    return 0
  fi
  if [[ -f "agent.sample.properties" ]]; then
    cp "agent.sample.properties" "agent.properties"
    echo "已由 agent.sample.properties 建立 agent.properties"
  fi
}

systemd_unit_exists() {
  command -v systemctl >/dev/null 2>&1 &&
    [[ "$(systemctl show "${SYSTEMD_UNIT}" --property=LoadState --value 2>/dev/null || true)" == "loaded" ]]
}

managed_systemctl() {
  if [[ "${EUID}" -eq 0 ]]; then
    systemctl "$@"
    return
  fi
  if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
    sudo -n systemctl "$@"
    return
  fi
  echo "偵測到 systemd 服務 ${SYSTEMD_UNIT}，但目前帳號沒有管理權限。" >&2
  echo "請使用 sudo 執行本腳本，或直接執行: sudo systemctl $*" >&2
  return 1
}

read_json_port() {
  local key="$1"
  if [[ ! -f "agent.properties" ]]; then
    return 0
  fi
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\\([0-9][0-9]*\\).*/\\1/p" agent.properties | head -n 1
}

port_pids() {
  local port="$1"
  if [[ -z "${port}" || "${port}" == "0" ]]; then
    return 0
  fi

  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -tiTCP:"${port}" -sTCP:LISTEN 2>/dev/null | sort -u
    return 0
  fi

  if command -v fuser >/dev/null 2>&1; then
    fuser "${port}/tcp" 2>/dev/null | tr ' ' '\n' | sed '/^$/d' | sort -u
    return 0
  fi

  if command -v ss >/dev/null 2>&1; then
    ss -ltnp 2>/dev/null | awk -v port=":${port}" '
      index($4, port) {
        while (match($0, /pid=[0-9]+/)) {
          print substr($0, RSTART + 4, RLENGTH - 4)
          $0 = substr($0, RSTART + RLENGTH)
        }
      }
    ' | sort -u
  fi
}

pid_alive() {
  local pid="$1"
  [[ "${pid}" =~ ^[0-9]+$ ]] && kill -0 "${pid}" 2>/dev/null
}

pid_belongs_to_service() {
  local pid="$1"
  local process_dir="" executable="" command_line="" first_arg=""

  pid_alive "${pid}" || return 1

  if [[ -d "/proc/${pid}" ]]; then
    process_dir="$(readlink "/proc/${pid}/cwd" 2>/dev/null || true)"
    executable="$(readlink "/proc/${pid}/exe" 2>/dev/null || true)"
    executable="${executable% (deleted)}"
    command_line="$(tr '\0' ' ' < "/proc/${pid}/cmdline" 2>/dev/null || true)"
    first_arg="${command_line%% *}"

    if [[ "${executable}" == "${ROOT_DIR}/${APP_NAME}" ]]; then
      return 0
    fi
    if [[ "${process_dir}" == "${ROOT_DIR}" ]] && {
      [[ "${first_arg}" == "./${APP_NAME}" ]] ||
      [[ "${first_arg}" == "${APP_NAME}" ]] ||
      [[ "${first_arg##*/}" == "${APP_NAME}" ]];
    }; then
      return 0
    fi
    return 1
  fi

  command_line="$(ps -p "${pid}" -o command= 2>/dev/null || true)"
  if [[ "${command_line}" == "${ROOT_DIR}/${APP_NAME}"* ]]; then
    return 0
  fi
  if [[ "${command_line}" == "./${APP_NAME}"* ]] && command -v lsof >/dev/null 2>&1; then
    process_dir="$(lsof -a -p "${pid}" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 1)"
    [[ "${process_dir}" == "${ROOT_DIR}" ]]
    return
  fi
  return 1
}

process_pids() {
  local process_path pid

  if [[ -d "/proc" ]]; then
    for process_path in /proc/[0-9]*; do
      [[ -d "${process_path}" ]] || continue
      pid="${process_path##*/}"
      if pid_belongs_to_service "${pid}"; then
        echo "${pid}"
      fi
    done
    return 0
  fi

  if command -v pgrep >/dev/null 2>&1; then
    while IFS= read -r pid; do
      if pid_belongs_to_service "${pid}"; then
        echo "${pid}"
      fi
    done < <(pgrep -f "${APP_NAME}" 2>/dev/null || true)
  fi
}

collect_pids() {
  local pid

  if [[ -f "${PID_FILE}" ]]; then
    pid="$(tr -cd '0-9' < "${PID_FILE}")"
    if pid_belongs_to_service "${pid}"; then
      echo "${pid}"
    fi
  fi

  process_pids

  local http_port https_port
  http_port="${HTTP_PORT:-$(read_json_port "http_port")}"
  https_port="${HTTPS_PORT:-$(read_json_port "https_port")}"
  port_pids "${http_port}"
  port_pids "${https_port}"
}

wait_pids_exit() {
  local deadline="$1"
  shift
  local pid

  while true; do
    local alive=0
    for pid in "$@"; do
      if pid_alive "${pid}"; then
        alive=1
        break
      fi
    done
    if [[ "${alive}" == "0" ]]; then
      return 0
    fi
    if (( $(date +%s) >= deadline )); then
      return 1
    fi
    sleep 0.3
  done
}

ensure_agent_properties

if systemd_unit_exists && systemctl is-active --quiet "${SYSTEMD_UNIT}"; then
  echo "停止 systemd 服務 ${SYSTEMD_UNIT}"
  if ! managed_systemctl stop "${SYSTEMD_UNIT}"; then
    exit 1
  fi
  if systemctl is-active --quiet "${SYSTEMD_UNIT}"; then
    echo "systemd 服務 ${SYSTEMD_UNIT} 仍在執行" >&2
    exit 1
  fi
  rm -f "${PID_FILE}"
  echo "${APP_NAME} 已由 systemd 停止"
  exit 0
fi

PIDS=()
while IFS= read -r pid; do
  [[ -n "${pid}" ]] && PIDS+=("${pid}")
done < <(collect_pids | sed '/^$/d' | sort -u)

FILTERED=()
for pid in "${PIDS[@]}"; do
  if [[ "${pid}" != "$$" && "${pid}" != "${PPID}" ]] && pid_alive "${pid}"; then
    FILTERED+=("${pid}")
  fi
done

if [[ "${#FILTERED[@]}" == "0" ]]; then
  echo "${APP_NAME} 未在執行中"
  rm -f "${PID_FILE}"
  exit 0
fi

echo "停止 ${APP_NAME}: ${FILTERED[*]}"
kill -TERM "${FILTERED[@]}" 2>/dev/null || true

if ! wait_pids_exit "$(( $(date +%s) + 8 ))" "${FILTERED[@]}"; then
  echo "程序未於期限內停止，改用 KILL: ${FILTERED[*]}"
  kill -KILL "${FILTERED[@]}" 2>/dev/null || true
  wait_pids_exit "$(( $(date +%s) + 3 ))" "${FILTERED[@]}" || true
fi

REMAINING=()
while IFS= read -r pid; do
  if [[ -n "${pid}" && "${pid}" != "$$" && "${pid}" != "${PPID}" ]] && pid_alive "${pid}"; then
    REMAINING+=("${pid}")
  fi
done < <(collect_pids | sed '/^$/d' | sort -u)

if [[ "${#REMAINING[@]}" -gt 0 ]]; then
  echo "無法停止 ${APP_NAME}，殘留 PID: ${REMAINING[*]}" >&2
  exit 1
fi

rm -f "${PID_FILE}"
echo "${APP_NAME} 已停止"
