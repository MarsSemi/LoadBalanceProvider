#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
APP_NAME="LoadBalanceProvider"
MAIN_PACKAGE="./src/cmd/loadbalanceprovider"
DIST_DIR="${ROOT_DIR}/dist"
WORK_DIR="${DIST_DIR}/work"
BIN_DIR="${ROOT_DIR}/bin"

cd "${ROOT_DIR}"

BUILD_TIME="$(date +%Y%m%d_%H%M%S)"
VERSION_TEXT="1.$(date +%y.%m%d) build $(date +%H%M)"
BUILT_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PACKAGE_NAME="${APP_NAME}_deploy_${BUILD_TIME}"
PACKAGE_DIR="${WORK_DIR}/${PACKAGE_NAME}"
MAC_BIN_NAME="${APP_NAME}_mac_arm64"
LINUX_BIN_NAME="${APP_NAME}_linux_x64"
LINUX_ARM_BIN_NAME="${APP_NAME}_linux_arm64"

echo "=== ${APP_NAME} 建置開始 ==="
echo "版本: ${VERSION_TEXT}"
echo "目標平台: mac arm64, linux x64, linux arm64"
echo "工作目錄: ${ROOT_DIR}"

require_file() {
  local path="$1"
  if [[ ! -e "${path}" ]]; then
    echo "缺少必要檔案或目錄: ${path}" >&2
    exit 1
  fi
}

require_command() {
  local name="$1"
  if ! command -v "${name}" >/dev/null 2>&1; then
    echo "缺少建置工具: ${name}" >&2
    exit 1
  fi
}

copy_if_exists() {
  local source="$1"
  local target="$2"
  if [[ -e "${source}" ]]; then
    mkdir -p "$(dirname "${target}")"
    cp -R "${source}" "${target}"
  fi
}

require_file "go.mod"
require_file "src/cmd/loadbalanceprovider/main.go"
require_file "website"
require_file "agent.sample.properties"
require_command "zip"
require_command "unzip"

mkdir -p "${DIST_DIR}"
echo "清空既有 zip 部署檔..."
find "${DIST_DIR}" -maxdepth 1 -type f -name "*.zip" -delete

echo "整理 Go module..."
go mod tidy

echo "編譯執行檔到 bin/..."
rm -rf "${WORK_DIR}"
mkdir -p "${PACKAGE_DIR}" "${BIN_DIR}"
rm -f "${BIN_DIR}/${MAC_BIN_NAME}" "${BIN_DIR}/${LINUX_BIN_NAME}" "${BIN_DIR}/${LINUX_ARM_BIN_NAME}"

GOOS="darwin" GOARCH="arm64" go build \
	-buildvcs=false \
  -trimpath \
  -ldflags "-s -w" \
  -o "${BIN_DIR}/${MAC_BIN_NAME}" \
  "${MAIN_PACKAGE}"

GOOS="linux" GOARCH="amd64" go build \
	-buildvcs=false \
  -trimpath \
  -ldflags "-s -w" \
  -o "${BIN_DIR}/${LINUX_BIN_NAME}" \
  "${MAIN_PACKAGE}"

GOOS="linux" GOARCH="arm64" go build \
	-buildvcs=false \
  -trimpath \
  -ldflags "-s -w" \
  -o "${BIN_DIR}/${LINUX_ARM_BIN_NAME}" \
  "${MAIN_PACKAGE}"

echo "複製部署檔案..."
copy_if_exists "agent.sample.properties" "${PACKAGE_DIR}/agent.sample.properties"
copy_if_exists "README.md" "${PACKAGE_DIR}/README.md"
copy_if_exists "DEPLOY.md" "${PACKAGE_DIR}/DEPLOY.md"
copy_if_exists "install.md" "${PACKAGE_DIR}/install.md"
copy_if_exists "stop.sh" "${PACKAGE_DIR}/stop.sh"
copy_if_exists "run_bg.sh" "${PACKAGE_DIR}/run_bg.sh"
copy_if_exists "website" "${PACKAGE_DIR}/website"
copy_if_exists "questBank" "${PACKAGE_DIR}/questBank"
copy_if_exists "bin" "${PACKAGE_DIR}/bin"
chmod +x "${PACKAGE_DIR}/stop.sh" "${PACKAGE_DIR}/run_bg.sh"

cat > "${PACKAGE_DIR}/install.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

APP_NAME="LoadBalanceProvider"
OS_NAME="$(uname -s)"
ARCH_NAME="$(uname -m)"
SOURCE_BIN=""

case "${OS_NAME}:${ARCH_NAME}" in
  Darwin:arm64)
    SOURCE_BIN="./bin/${APP_NAME}_mac_arm64"
    ;;
  Linux:x86_64|Linux:amd64)
    SOURCE_BIN="./bin/${APP_NAME}_linux_x64"
    ;;
  Linux:aarch64|Linux:arm64)
    SOURCE_BIN="./bin/${APP_NAME}_linux_arm64"
    ;;
  *)
    echo "不支援的平台: ${OS_NAME}/${ARCH_NAME}" >&2
    echo "目前封裝只包含 mac arm64、linux x64 與 linux arm64。" >&2
    exit 1
    ;;
esac

if [[ ! -x "${SOURCE_BIN}" ]]; then
  echo "找不到可執行檔: ${SOURCE_BIN}" >&2
  exit 1
fi

cp "${SOURCE_BIN}" "./${APP_NAME}"
chmod +x "./${APP_NAME}"
echo "已安裝符合平台的執行檔: ${SOURCE_BIN} -> ./${APP_NAME}"
exec "./${APP_NAME}"
EOF
chmod +x "${PACKAGE_DIR}/install.sh"

cat > "${PACKAGE_DIR}/run.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
cd "\$(dirname "\$0")"
exec ./install.sh
EOF
chmod +x "${PACKAGE_DIR}/run.sh"

cat > "${PACKAGE_DIR}/run.command" <<'EOF'
#!/usr/bin/env bash
cd "$(dirname "$0")" || exit 1
./run.sh
EOF
chmod +x "${PACKAGE_DIR}/run.command"

cat > "${PACKAGE_DIR}/BUILD_INFO.txt" <<EOF
app=${APP_NAME}
version=${VERSION_TEXT}
targets=darwin/arm64,linux/amd64,linux/arm64
built_at=${BUILT_AT}
entry=./install.sh 或 ./run.command 或 ./run.sh
background_entry=./run_bg.sh
stop_entry=./stop.sh
EOF

cat > "${PACKAGE_DIR}/build-info.json" <<EOF
{
  "service": "${APP_NAME}",
  "version": "${VERSION_TEXT}",
  "build_time": "${BUILT_AT}",
  "targets": [
    "darwin/arm64",
    "linux/amd64",
    "linux/arm64"
  ]
}
EOF

echo "建立部署壓縮檔..."
ARTIFACT="${DIST_DIR}/${PACKAGE_NAME}.zip"
(
  cd "${WORK_DIR}"
  zip -qry "${ARTIFACT}" "${PACKAGE_NAME}"
)

echo "驗證部署壓縮檔..."
unzip -tq "${ARTIFACT}" >/dev/null

verify_archive_file() {
  local source="$1"
  local archive_path="$2"
  if ! unzip -p "${ARTIFACT}" "${PACKAGE_NAME}/${archive_path}" | cmp -s - "${source}"; then
    echo "部署檔案內容不一致: ${archive_path}" >&2
    exit 1
  fi
}

verify_archive_file "website/main.html" "website/main.html"
verify_archive_file "website/system-monitor.html" "website/system-monitor.html"
verify_archive_file "run_bg.sh" "run_bg.sh"
verify_archive_file "stop.sh" "stop.sh"

ARCHIVE_ENTRIES="$(unzip -Z1 "${ARTIFACT}")"
for required_path in \
  "build-info.json" \
  "website/main.html" \
  "website/system-monitor.html" \
  "run_bg.sh" \
  "stop.sh" \
  "bin/${MAC_BIN_NAME}" \
  "bin/${LINUX_BIN_NAME}" \
  "bin/${LINUX_ARM_BIN_NAME}"; do
  if ! grep -Fxq "${PACKAGE_NAME}/${required_path}" <<< "${ARCHIVE_ENTRIES}"; then
    echo "部署壓縮檔缺少必要檔案: ${required_path}" >&2
    exit 1
  fi
done

if grep -Eq "^${PACKAGE_NAME}/data(/|$)|^${PACKAGE_NAME}/agent\.properties$|\.bak$" <<< "${ARCHIVE_ENTRIES}"; then
  echo "部署壓縮檔包含禁止封裝的執行資料、agent.properties 或 .bak 檔案" >&2
  exit 1
fi

if ! unzip -p "${ARTIFACT}" "${PACKAGE_NAME}/website/main.html" | grep -F '<span>系統更新</span>' >/dev/null; then
  echo "部署壓縮檔中的系統更新按鈕不是目前版本" >&2
  exit 1
fi

echo "完整部署壓縮檔驗證通過"

echo "清理暫存..."
rm -rf "${WORK_DIR}"

echo "=== 建置完成 ==="
echo "完整部署檔案: ${ARTIFACT}"
