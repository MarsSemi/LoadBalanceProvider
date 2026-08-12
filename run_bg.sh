#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")"

APP_NAME="LoadBalanceProvider"
PID_FILE="run.pid"
LOG_FILE="${LOG_FILE:-service.log}"
SYSTEMD_UNIT="${LBP_SYSTEMD_UNIT:-load-balance-provider.service}"

ensure_agent_properties() {
  if [[ -f "agent.properties" ]]; then
    return 0
  fi
  if [[ ! -f "agent.sample.properties" ]]; then
    echo "缺少 agent.properties，且找不到 agent.sample.properties" >&2
    exit 1
  fi
  cp "agent.sample.properties" "agent.properties"
  echo "已由 agent.sample.properties 建立 agent.properties"
}

install_runtime_binary() {
  local os_name arch_name source_bin
  os_name="$(uname -s)"
  arch_name="$(uname -m)"
  source_bin=""

  case "${os_name}:${arch_name}" in
    Darwin:arm64)
      source_bin="./bin/${APP_NAME}_mac_arm64"
      ;;
    Linux:x86_64|Linux:amd64)
      source_bin="./bin/${APP_NAME}_linux_x64"
      ;;
    Linux:aarch64|Linux:arm64)
      source_bin="./bin/${APP_NAME}_linux_arm64"
      ;;
    *)
      echo "不支援的平台: ${os_name}/${arch_name}" >&2
      exit 1
      ;;
  esac

  if [[ ! -x "${source_bin}" ]]; then
    echo "找不到可執行檔: ${source_bin}" >&2
    exit 1
  fi

  cp "${source_bin}" "./${APP_NAME}"
  chmod +x "./${APP_NAME}"
  echo "已安裝符合平台的執行檔: ${source_bin} -> ./${APP_NAME}"
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

if [[ ! -x "./${APP_NAME}" ]]; then
  install_runtime_binary
fi

ensure_agent_properties

if systemd_unit_exists; then
  echo "偵測到 systemd 服務 ${SYSTEMD_UNIT}，交由 systemd 重新啟動。"
  if ! managed_systemctl restart "${SYSTEMD_UNIT}"; then
    exit 1
  fi
  sleep "${STARTUP_WAIT_SECONDS:-3}"
  if ! systemctl is-active --quiet "${SYSTEMD_UNIT}"; then
    echo "systemd 服務 ${SYSTEMD_UNIT} 啟動失敗" >&2
    systemctl status "${SYSTEMD_UNIT}" --no-pager -n 30 >&2 || true
    exit 1
  fi
  rm -f "${PID_FILE}"
  managed_pid="$(systemctl show "${SYSTEMD_UNIT}" --property=MainPID --value 2>/dev/null || true)"
  echo "${APP_NAME} 已由 systemd 啟動，PID=${managed_pid:-unknown}"
  echo "Log: journalctl -u ${SYSTEMD_UNIT}"
  exit 0
fi

if [[ ! -x "./stop.sh" ]]; then
  echo "找不到可執行的 stop.sh，為避免重複啟動而中止" >&2
  exit 1
fi

if ! ./stop.sh; then
  echo "舊程序未能完整停止，不啟動新的 ${APP_NAME}" >&2
  exit 1
fi

: > "${LOG_FILE}"
nohup "./${APP_NAME}" > "${LOG_FILE}" 2>&1 &
pid="$!"
echo "${pid}" > "${PID_FILE}"

sleep "${STARTUP_WAIT_SECONDS:-3}"
if kill -0 "${pid}" 2>/dev/null; then
  echo "${APP_NAME} 已在背景啟動，PID=${pid}"
  echo "Log: $(pwd)/${LOG_FILE}"
else
  rm -f "${PID_FILE}"
  echo "${APP_NAME} 啟動失敗，請檢查 ${LOG_FILE}" >&2
  if [[ -s "${LOG_FILE}" ]]; then
    tail -n 30 "${LOG_FILE}" >&2
  fi
  exit 1
fi
