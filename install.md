# LoadBalanceProvider 安裝說明

## 封裝內容

部署 zip 內會包含三個平台的執行檔：

- `bin/LoadBalanceProvider_mac_arm64`：macOS Apple Silicon。
- `bin/LoadBalanceProvider_linux_x64`：Linux x86_64 / amd64。
- `bin/LoadBalanceProvider_linux_arm64`：Linux arm64 / aarch64。

正式執行檔不直接放在根目錄，需透過 `install.sh` 依照目前 OS 與 CPU 架構複製。

## 安裝與啟動

解壓縮部署檔後，進入目錄執行：

```bash
./install.sh
```

`install.sh` 會自動偵測平台，將符合的平台執行檔複製為：

```text
./LoadBalanceProvider
```

接著會直接執行 `./LoadBalanceProvider`。

也可以執行：

```bash
./run.sh
```

若要在背景執行：

```bash
./run_bg.sh
```

`run_bg.sh` 會自動安裝符合平台的執行檔、建立缺少的 `agent.properties`、停止既有程序，並將輸出寫入：

```text
service.log
```

若要停止背景服務：

```bash
./stop.sh
```

macOS 桌面環境可直接執行：

```bash
./run.command
```

## 設定檔

封裝內只包含：

```text
agent.sample.properties
```

第一次啟動時，如果目錄中不存在 `agent.properties`，系統會自動由 `agent.sample.properties` 複製建立：

```text
agent.sample.properties -> agent.properties
```

部署後請依實際環境修改 `agent.properties`，例如 Mars Cloud 連線資訊、HTTP port、HTTPS port 與 web path。

## 支援平台

目前 `install.sh` 支援：

- macOS arm64
- Linux x86_64 / amd64
- Linux arm64 / aarch64

其他平台會停止並顯示不支援訊息。若要支援其他架構，需要在 `build.sh` 增加對應的 `GOOS/GOARCH` 編譯目標，並同步更新 `install.sh` 的平台判斷。
