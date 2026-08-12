(function () {
  "use strict";

  const STORAGE_KEY = "lbp.language";
  const CHANNEL_NAME = "lbp-language";
  const SUPPORTED_PREFERENCES = new Set(["auto", "zh-TW", "zh-CN", "en", "ja", "ko"]);
  const textSources = new WeakMap();
  const renderedTexts = new WeakMap();
  const attributeSources = new WeakMap();
  let preference = normalizePreference(localStorage.getItem(STORAGE_KEY));
  let locale = resolveLocale(preference);
  let observer = null;
  let channel = null;

  // 每列依序為：繁體中文原文、English、简体中文、日本語、한국어。
  const rows = [
    ["LLM Proxy 儀表版", "LLM Proxy Dashboard", "LLM Proxy 仪表板", "LLM Proxy ダッシュボード", "LLM Proxy 대시보드"],
    ["LLM Proxy 登入", "LLM Proxy Sign In", "LLM Proxy 登录", "LLM Proxy ログイン", "LLM Proxy 로그인"],
    ["LLM Provider 管理", "LLM Provider Management", "LLM Provider 管理", "LLM Provider 管理", "LLM Provider 관리"],
    ["主選單", "Main menu", "主菜单", "メインメニュー", "주 메뉴"],
    ["儀表版", "Dashboard", "仪表板", "ダッシュボード", "대시보드"],
    ["模型來源", "Providers", "模型来源", "プロバイダー", "모델 공급자"],
    ["基準", "Benchmarks", "基准", "ベンチマーク", "벤치마크"],
    ["基準測試", "Benchmarks", "基准测试", "ベンチマーク", "벤치마크"],
    ["系統監看", "System Monitor", "系统监控", "システムモニター", "시스템 모니터"],
    ["系統資源監看", "System Resource Monitor", "系统资源监控", "システムリソースモニター", "시스템 리소스 모니터"],
    ["金鑰管理", "API Key Management", "密钥管理", "API キー管理", "API 키 관리"],
    ["設定", "Settings", "设置", "設定", "설정"],
    ["設定功能", "Settings sections", "设置功能", "設定項目", "설정 항목"],
    ["一般", "General", "常规", "一般", "일반"],
    ["進階", "Advanced", "高级", "詳細設定", "고급"],
    ["MCP", "MCP", "MCP", "MCP", "MCP"],
    ["Streamable HTTP 端點", "Streamable HTTP Endpoint", "Streamable HTTP 端点", "Streamable HTTP エンドポイント", "Streamable HTTP 엔드포인트"],
    ["MCP 連線位址", "MCP Connection URL", "MCP 连接地址", "MCP 接続 URL", "MCP 연결 URL"],
    ["請先在金鑰管理核發 MCP 專用金鑰，再透過 Authorization: Bearer 或 X-API-Key 連線。Chat 與登入金鑰無法呼叫 MCP。", "Issue a dedicated MCP key in Key Management first, then connect through Authorization: Bearer or X-API-Key. Chat and login keys cannot call MCP.", "请先在密钥管理中签发 MCP 专用密钥，再通过 Authorization: Bearer 或 X-API-Key 连接。Chat 与登录密钥无法调用 MCP。", "先にキー管理で MCP 専用キーを発行し、Authorization: Bearer または X-API-Key で接続してください。Chat キーとログインキーでは MCP を呼び出せません。", "먼저 키 관리에서 MCP 전용 키를 발급한 후 Authorization: Bearer 또는 X-API-Key로 연결하세요. Chat 및 로그인 키로는 MCP를 호출할 수 없습니다."],
    ["複製位址", "Copy URL", "复制地址", "URL をコピー", "URL 복사"],
    ["傳輸", "Transport", "传输", "トランスポート", "전송 방식"],
    ["協定版本", "Protocol Version", "协议版本", "プロトコルバージョン", "프로토콜 버전"],
    ["可用工具", "Available Tools", "可用工具", "利用可能なツール", "사용 가능한 도구"],
    ["啟用 MCP", "Enable MCP", "启用 MCP", "MCP を有効化", "MCP 활성화"],
    ["開啟標準 MCP Streamable HTTP 端點。關閉後，既有 MCP 連線會收到服務未啟用錯誤。", "Enable the standard MCP Streamable HTTP endpoint. Existing MCP connections receive a service-disabled error when it is off.", "开启标准 MCP Streamable HTTP 端点。关闭后，现有 MCP 连接会收到服务未启用错误。", "標準 MCP Streamable HTTP エンドポイントを有効にします。無効時、既存の MCP 接続にはサービス無効エラーが返ります。", "표준 MCP Streamable HTTP 엔드포인트를 활성화합니다. 비활성화하면 기존 MCP 연결에 서비스 비활성 오류가 반환됩니다."],
    ["唯讀模式", "Read-only Mode", "只读模式", "読み取り専用モード", "읽기 전용 모드"],
    ["MCP 唯讀模式", "MCP read-only mode", "MCP 只读模式", "MCP 読み取り専用モード", "MCP 읽기 전용 모드"],
    ["只公開查詢工具；Provider、設定、基準測試、通知、推論與系統更新等會改變狀態的工具不會出現在 tools/list。", "Expose query tools only. State-changing tools for Providers, settings, benchmarks, notifications, inference, and system updates are omitted from tools/list.", "仅公开查询工具；Provider、设置、基准测试、通知、推理与系统更新等会改变状态的工具不会出现在 tools/list。", "照会ツールのみ公開します。プロバイダー、設定、ベンチマーク、通知、推論、システム更新など状態を変更するツールは tools/list に表示されません。", "조회 도구만 공개합니다. 공급자, 설정, 벤치마크, 알림, 추론, 시스템 업데이트 등 상태를 변경하는 도구는 tools/list에 표시되지 않습니다."],
    ["允許的瀏覽器 Origin", "Allowed Browser Origins", "允许的浏览器 Origin", "許可するブラウザー Origin", "허용된 브라우저 Origin"],
    ["額外允許 Origin（每行一個）", "Additional allowed origins (one per line)", "额外允许的 Origin（每行一个）", "追加で許可する Origin（1 行に 1 件）", "추가 허용 Origin(한 줄에 하나)"],
    ["未帶 Origin 的原生 MCP Client 與同源瀏覽器請求會自動允許；跨來源瀏覽器連線必須列在此處，以防止 DNS rebinding。", "Native MCP clients without an Origin and same-origin browser requests are allowed automatically. Cross-origin browser connections must be listed here to prevent DNS rebinding.", "未带 Origin 的原生 MCP Client 与同源浏览器请求会自动允许；跨来源浏览器连接必须列在此处，以防止 DNS rebinding。", "Origin のないネイティブ MCP クライアントと同一オリジンのブラウザー要求は自動許可されます。DNS rebinding 防止のため、クロスオリジン接続はここに登録してください。", "Origin이 없는 네이티브 MCP 클라이언트와 동일 출처 브라우저 요청은 자동 허용됩니다. DNS rebinding 방지를 위해 교차 출처 연결은 여기에 등록해야 합니다."],
    ["目前公開工具", "Currently Exposed Tools", "当前公开工具", "現在公開中のツール", "현재 공개 도구"],
    ["金鑰管理固定排除，不受唯讀模式或其他設定影響。", "API-key management is always excluded, regardless of read-only mode or other settings.", "密钥管理固定排除，不受只读模式或其他设置影响。", "API キー管理は読み取り専用モードや他の設定に関係なく常に除外されます。", "API 키 관리는 읽기 전용 모드나 다른 설정과 관계없이 항상 제외됩니다."],
    ["儲存 MCP 設定", "Save MCP Settings", "保存 MCP 设置", "MCP 設定を保存", "MCP 설정 저장"],
    ["尚未載入 MCP 設定。", "MCP settings have not been loaded.", "尚未加载 MCP 设置。", "MCP 設定はまだ読み込まれていません。", "MCP 설정을 아직 불러오지 않았습니다."],
    ["MCP 設定已載入。", "MCP settings loaded.", "MCP 设置已加载。", "MCP 設定を読み込みました。", "MCP 설정을 불러왔습니다."],
    ["無法載入 MCP 設定。", "Unable to load MCP settings.", "无法加载 MCP 设置。", "MCP 設定を読み込めません。", "MCP 설정을 불러올 수 없습니다."],
    ["MCP 設定儲存中。", "Saving MCP settings.", "正在保存 MCP 设置。", "MCP 設定を保存中です。", "MCP 설정을 저장하는 중입니다."],
    ["MCP 設定已儲存並立即生效。", "MCP settings saved and applied immediately.", "MCP 设置已保存并立即生效。", "MCP 設定を保存し、すぐに反映しました。", "MCP 설정을 저장하고 즉시 적용했습니다."],
    ["儲存 MCP 設定失敗。", "Failed to save MCP settings.", "保存 MCP 设置失败。", "MCP 設定の保存に失敗しました。", "MCP 설정 저장에 실패했습니다."],
    ["MCP 連線位址已複製。", "MCP connection URL copied.", "MCP 连接地址已复制。", "MCP 接続 URL をコピーしました。", "MCP 연결 URL을 복사했습니다."],
    ["無法複製 MCP 連線位址。", "Unable to copy the MCP connection URL.", "无法复制 MCP 连接地址。", "MCP 接続 URL をコピーできません。", "MCP 연결 URL을 복사할 수 없습니다."],
    ["目前沒有公開工具", "No tools are currently exposed", "当前没有公开工具", "現在公開中のツールはありません", "현재 공개된 도구가 없습니다"],
    ["正在重新載入 MCP 設定...", "Reloading MCP settings...", "正在重新加载 MCP 设置...", "MCP 設定を再読み込み中...", "MCP 설정을 다시 불러오는 중..."],
    ["登出", "Sign out", "退出登录", "ログアウト", "로그아웃"],
    ["切換外觀", "Switch appearance", "切换外观", "外観を切り替え", "화면 모드 전환"],
    ["服務統計", "Service Statistics", "服务统计", "サービス統計", "서비스 통계"],
    ["統計區間", "Statistics range", "统计区间", "統計範囲", "통계 범위"],
    ["工作階段", "Session", "会话", "セッション", "세션"],
    ["累計", "Cumulative", "累计", "累計", "누적"],
    ["歸零", "Reset", "归零", "リセット", "초기화"],
    ["所有模型", "All models", "所有模型", "すべてのモデル", "모든 모델"],
    ["Provider 篩選", "Provider filter", "Provider 筛选", "プロバイダーフィルター", "공급자 필터"],
    ["核心指標", "Key metrics", "核心指标", "主要指標", "핵심 지표"],
    ["Provider 總數", "Total providers", "Provider 总数", "プロバイダー総数", "전체 공급자"],
    ["啟用 Provider", "Enabled providers", "启用的 Provider", "有効なプロバイダー", "활성 공급자"],
    ["目前活躍請求", "Active requests", "当前活跃请求", "現在のアクティブリクエスト", "현재 활성 요청"],
    ["累積回應 Token 數", "Cumulative response tokens", "累计响应 Token 数", "累計応答トークン数", "누적 응답 토큰"],
    ["累積請求", "Cumulative requests", "累计请求", "累計リクエスト", "누적 요청"],
    ["可使用量", "Available quota", "可用配额", "利用可能量", "사용 가능량"],
    ["查看每日總用量百分比", "View daily total usage percentage", "查看每日总用量百分比", "日別合計使用率を表示", "일일 총 사용률 보기"],
    ["查看可使用量的每日用量百分比", "View daily usage percentage of available quota", "查看可用配额的每日用量百分比", "利用可能量の日別使用率を表示", "사용 가능량의 일일 사용률 보기"],
    ["無限制", "Unlimited", "无限制", "無制限", "무제한"],
    ["平均速度", "Average Speed", "平均速度", "平均速度", "평균 속도"],
    ["反應時間", "Response time", "响应时间", "応答時間", "응답 시간"],
    ["平均處理時間", "Average processing time", "平均处理时间", "平均処理時間", "평균 처리 시간"],
    ["Token 生成速度", "Token generation speed", "Token 生成速度", "トークン生成速度", "토큰 생성 속도"],
    ["Token 輸出速度", "Token output speed", "Token 输出速度", "トークン出力速度", "토큰 출력 속도"],
    ["活躍模型", "Active Models", "活跃模型", "アクティブモデル", "활성 모델"],
    ["Provider 列表移動", "Move provider list", "移动 Provider 列表", "プロバイダー一覧を移動", "공급자 목록 이동"],
    ["向左移動 Provider 列表", "Move provider list left", "向左移动 Provider 列表", "プロバイダー一覧を左へ移動", "공급자 목록 왼쪽 이동"],
    ["向右移動 Provider 列表", "Move provider list right", "向右移动 Provider 列表", "プロバイダー一覧を右へ移動", "공급자 목록 오른쪽 이동"],
    ["尚未載入模型", "Models have not been loaded", "尚未加载模型", "モデルはまだ読み込まれていません", "모델을 아직 불러오지 않았습니다"],
    ["載入版本", "Loading version", "加载版本", "バージョンを読み込み中", "버전 불러오는 중"],
    ["未指定模型", "Model not specified", "未指定模型", "モデル未指定", "모델 미지정"],
    ["請求數", "Requests", "请求数", "リクエスト数", "요청 수"],
    ["綁定數", "Bound", "绑定数", "バインド数", "바인딩 수"],
    ["活躍數", "Active", "活跃数", "アクティブ数", "활성 수"],
    ["狀態", "Status", "状态", "状態", "상태"],
    ["錯誤", "Error", "错误", "エラー", "오류"],
    ["更新時間", "Updated", "更新时间", "更新時刻", "업데이트 시간"],
    ["剩餘用量", "Remaining quota", "剩余用量", "残り使用量", "남은 사용량"],
    ["剩餘", "Remaining", "剩余", "残り", "남음"],
    ["登入 TOKEN 已失效", "Login token has expired", "登录 TOKEN 已失效", "ログイントークンの有効期限が切れました", "로그인 토큰이 만료되었습니다"],
    ["請求", "Requests", "请求", "リクエスト", "요청"],
    ["請求重置", "Request reset", "请求重置", "リクエストリセット", "요청 초기화"],
    ["Token 重置", "Token reset", "Token 重置", "トークンリセット", "토큰 초기화"],
    ["方案", "Plan", "方案", "プラン", "요금제"],
    ["限制", "Limit", "限制", "制限", "제한"],
    ["無", "None", "无", "なし", "없음"],
    ["尚未載入", "Not loaded", "尚未加载", "未読み込み", "아직 불러오지 않음"],
    ["無相關資訊", "No information available", "无相关信息", "関連情報はありません", "관련 정보 없음"],

    ["模型列表顯示", "Model List Display", "模型列表显示", "モデル一覧表示", "모델 목록 표시"],
    ["關閉時，對外模型列表只顯示 AUTO。開啟時，會列出所有啟用來源提供的模型；同名模型仍由負載平衡自動挑選。", "When off, the public model list only shows AUTO. When on, it lists models from all enabled providers; models with the same name are still selected automatically by load balancing.", "关闭时，对外模型列表只显示 AUTO。开启时，会列出所有已启用来源提供的模型；同名模型仍由负载均衡自动选择。", "オフの場合、公開モデル一覧には AUTO のみ表示されます。オンの場合、有効な全プロバイダーのモデルを表示し、同名モデルは引き続きロードバランサーが自動選択します。", "끄면 외부 모델 목록에 AUTO만 표시합니다. 켜면 활성 공급자의 모든 모델을 표시하며, 같은 이름의 모델은 부하 분산이 자동 선택합니다."],
    ["列出來源支援模型", "List models supported by providers", "列出来源支持的模型", "プロバイダー対応モデルを一覧表示", "공급자가 지원하는 모델 표시"],
    ["介面語言", "Interface Language", "界面语言", "表示言語", "인터페이스 언어"],
    ["選擇介面顯示語言。Auto 會依瀏覽器或作業系統語言自動判斷。", "Choose the interface language. Auto follows your browser or operating system language.", "选择界面显示语言。Auto 会根据浏览器或操作系统语言自动判断。", "表示言語を選択します。Auto はブラウザーまたは OS の言語に合わせます。", "인터페이스 표시 언어를 선택합니다. Auto는 브라우저 또는 운영 체제 언어를 따릅니다."],
    ["選擇介面語言", "Select interface language", "选择界面语言", "表示言語を選択", "인터페이스 언어 선택"],
    ["版本資訊", "Version Information", "版本信息", "バージョン情報", "버전 정보"],
    ["版本", "Version", "版本", "バージョン", "버전"],
    ["服務", "Service", "服务", "サービス", "서비스"],
    ["啟動時間", "Started", "启动时间", "起動時刻", "시작 시간"],
    ["載入中", "Loading", "加载中", "読み込み中", "불러오는 중"],
    ["尚未載入一般設定。", "General settings have not been loaded.", "尚未加载常规设置。", "一般設定はまだ読み込まれていません。", "일반 설정을 아직 불러오지 않았습니다."],
    ["一般設定已載入。", "General settings loaded.", "常规设置已加载。", "一般設定を読み込みました。", "일반 설정을 불러왔습니다."],
    ["一般設定使用預設值。", "Using default general settings.", "常规设置使用默认值。", "一般設定は既定値を使用します。", "기본 일반 설정을 사용합니다."],
    ["無法載入一般設定。", "Unable to load general settings.", "无法加载常规设置。", "一般設定を読み込めません。", "일반 설정을 불러올 수 없습니다."],
    ["一般設定儲存中。", "Saving general settings.", "正在保存常规设置。", "一般設定を保存中です。", "일반 설정을 저장하는 중입니다."],
    ["模型列表會顯示 AUTO 與來源模型。", "The model list will show AUTO and provider models.", "模型列表会显示 AUTO 与来源模型。", "モデル一覧に AUTO とプロバイダーのモデルを表示します。", "모델 목록에 AUTO와 공급자 모델을 표시합니다."],
    ["模型列表只顯示 AUTO。", "The model list only shows AUTO.", "模型列表只显示 AUTO。", "モデル一覧には AUTO のみ表示します。", "모델 목록에 AUTO만 표시합니다."],
    ["目前後端尚未支援一般設定儲存。", "The backend does not currently support saving general settings.", "当前后端尚未支持保存常规设置。", "現在のバックエンドは一般設定の保存に対応していません。", "현재 백엔드는 일반 설정 저장을 지원하지 않습니다."],
    ["儲存一般設定失敗。", "Failed to save general settings.", "保存常规设置失败。", "一般設定の保存に失敗しました。", "일반 설정 저장에 실패했습니다."],
    ["對話黏著與路由快取", "Conversation Affinity and Route Cache", "对话粘性与路由缓存", "会話アフィニティとルートキャッシュ", "대화 고정 및 경로 캐시"],
    ["對話黏著時間（分鐘）", "Conversation affinity (minutes)", "对话粘性时间（分钟）", "会話アフィニティ時間（分）", "대화 고정 시간(분)"],
    ["配額容忍差（百分點）", "Quota tolerance (percentage points)", "配额容忍差（百分点）", "クォータ許容差（ポイント）", "할당량 허용 차이(%p)"],
    ["Response 路由上限", "Response route limit", "Response 路由上限", "Response ルート上限", "Response 경로 한도"],
    ["容量冷卻（秒）", "Capacity cooldown (seconds)", "容量冷却（秒）", "容量クールダウン（秒）", "용량 대기 시간(초)"],
    ["route 存在時會優先沿用原 Provider；route 遺失、過期、配額差超過容忍值，或原 Provider 正在容量冷卻中時，會移除舊 continuity 後重新負載平衡。容量冷卻是上游回報限流／容量錯誤後暫停選用該 Provider 的秒數，調短可減少對話被迫降級的機會。設定儲存後立即生效。", "An existing route reuses its original provider first. If the route is missing or expired, the quota gap exceeds tolerance, or the original provider is cooling down, the old continuity is removed and load balancing runs again. Capacity cooldown is how long a provider is paused after an upstream rate-limit or capacity error. Changes take effect immediately after saving.", "存在 route 时会优先沿用原 Provider；当 route 遗失、过期、配额差超过容忍值，或原 Provider 正在容量冷却时，会移除旧 continuity 后重新进行负载均衡。容量冷却是上游返回限流／容量错误后暂停选择该 Provider 的秒数。设置保存后立即生效。", "route がある場合は元のプロバイダーを優先します。route の欠落・期限切れ、クォータ差の超過、または元のプロバイダーが容量クールダウン中の場合、古い continuity を削除して再度ロードバランスします。保存後すぐに反映されます。", "route가 있으면 기존 공급자를 우선 사용합니다. route가 없거나 만료됨, 할당량 차이가 허용치를 초과함, 기존 공급자가 용량 대기 중인 경우 이전 continuity를 제거하고 다시 부하 분산합니다. 저장 즉시 적용됩니다."],
    ["重新載入", "Reload", "重新加载", "再読み込み", "다시 불러오기"],
    ["儲存參數", "Save Parameters", "保存参数", "パラメーターを保存", "매개변수 저장"],
    ["尚未載入進階設定。", "Advanced settings have not been loaded.", "尚未加载高级设置。", "詳細設定はまだ読み込まれていません。", "고급 설정을 아직 불러오지 않았습니다."],
    ["進階設定已載入。", "Advanced settings loaded.", "高级设置已加载。", "詳細設定を読み込みました。", "고급 설정을 불러왔습니다."],
    ["目前後端尚未支援進階設定。", "The backend does not currently support advanced settings.", "当前后端尚未支持高级设置。", "現在のバックエンドは詳細設定に対応していません。", "현재 백엔드는 고급 설정을 지원하지 않습니다."],
    ["無法載入進階設定。", "Unable to load advanced settings.", "无法加载高级设置。", "詳細設定を読み込めません。", "고급 설정을 불러올 수 없습니다."],
    ["進階設定儲存中。", "Saving advanced settings.", "正在保存高级设置。", "詳細設定を保存中です。", "고급 설정을 저장하는 중입니다."],
    ["進階設定已儲存並立即生效。", "Advanced settings saved and applied immediately.", "高级设置已保存并立即生效。", "詳細設定を保存し、すぐに反映しました。", "고급 설정을 저장하고 즉시 적용했습니다."],
    ["儲存進階設定失敗。", "Failed to save advanced settings.", "保存高级设置失败。", "詳細設定の保存に失敗しました。", "고급 설정 저장에 실패했습니다."],
    ["通知目標", "Notification Target", "通知目标", "通知先", "알림 대상"],
    ["通知 URL", "Notification URL", "通知 URL", "通知 URL", "알림 URL"],
    ["已設定時留空表示保留原金鑰", "Leave blank to keep the existing key", "已设置时留空表示保留原密钥", "設定済みの場合、空欄で既存キーを保持", "설정된 경우 비워 두면 기존 키 유지"],
    ["通知內容會替換 payload 中的", "Notification content replaces the following placeholder in the payload:", "通知内容会替换 payload 中的", "通知内容は payload 内の次の値を置換します：", "알림 내용은 payload에서 다음 값을 대체합니다:"],
    ["測試", "Test", "测试", "テスト", "테스트"],
    ["儲存通知目標", "Save Notification Target", "保存通知目标", "通知先を保存", "알림 대상 저장"],
    ["尚未載入通知設定。", "Notification settings have not been loaded.", "尚未加载通知设置。", "通知設定はまだ読み込まれていません。", "알림 설정을 아직 불러오지 않았습니다."],
    ["通知設定已載入。", "Notification settings loaded.", "通知设置已加载。", "通知設定を読み込みました。", "알림 설정을 불러왔습니다."],
    ["無法載入通知設定。", "Unable to load notification settings.", "无法加载通知设置。", "通知設定を読み込めません。", "알림 설정을 불러올 수 없습니다."],
    ["系統更新", "System Update", "系统更新", "システム更新", "시스템 업데이트"],
    ["上傳 ZIP 更新系統", "Upload a ZIP to update the system", "上传 ZIP 更新系统", "ZIP をアップロードしてシステムを更新", "ZIP을 업로드하여 시스템 업데이트"],
    ["更新 LLM Proxy", "Update LLM Proxy", "更新 LLM Proxy", "LLM Proxy を更新", "LLM Proxy 업데이트"],
    ["尚未選擇檔案", "No file selected", "尚未选择文件", "ファイルが選択されていません", "선택한 파일 없음"],
    ["系統會驗證部署包、備份現有程式並重新啟動服務。Provider、金鑰、通知與歷史資料不會被更新包覆蓋。", "The system validates the deployment package, backs up the current application, and restarts the service. Providers, keys, notifications, and history are preserved.", "系统会验证部署包、备份现有程序并重新启动服务。Provider、密钥、通知与历史数据不会被更新包覆盖。", "デプロイパッケージを検証し、現在のプログラムをバックアップしてサービスを再起動します。プロバイダー、キー、通知、履歴データは保持されます。", "배포 패키지를 검증하고 현재 프로그램을 백업한 뒤 서비스를 다시 시작합니다. 공급자, 키, 알림 및 기록 데이터는 유지됩니다."],
    ["取消", "Cancel", "取消", "キャンセル", "취소"],
    ["上傳並更新", "Upload and Update", "上传并更新", "アップロードして更新", "업로드 및 업데이트"],
    ["確認歸零", "Confirm Reset", "确认归零", "リセットを確認", "초기화 확인"],
    ["確認刪除", "Confirm Delete", "确认删除", "削除を確認", "삭제 확인"],
    ["關閉", "Close", "关闭", "閉じる", "닫기"],
    ["儲存", "Save", "保存", "保存", "저장"],
    ["確認", "Confirm", "确认", "確認", "확인"],
    ["正在載入", "Loading", "正在加载", "読み込み中", "불러오는 중"],
    ["正在載入...", "Loading...", "正在加载...", "読み込み中...", "불러오는 중..."],
    ["正在取得服務資訊...", "Getting service information...", "正在获取服务信息...", "サービス情報を取得中...", "서비스 정보를 가져오는 중..."],
    ["正在驗證登入狀態...", "Verifying sign-in status...", "正在验证登录状态...", "ログイン状態を確認中...", "로그인 상태 확인 중..."],
    ["正在取得 Provider 與服務統計...", "Getting providers and service statistics...", "正在获取 Provider 与服务统计...", "プロバイダーとサービス統計を取得中...", "공급자 및 서비스 통계를 가져오는 중..."],

    ["來源列表", "Provider List", "来源列表", "プロバイダー一覧", "공급자 목록"],
    ["Provider 列表", "Provider list", "Provider 列表", "プロバイダー一覧", "공급자 목록"],
    ["Provider 內容", "Provider details", "Provider 内容", "プロバイダー内容", "공급자 내용"],
    ["新增 Provider", "Add provider", "新增 Provider", "プロバイダーを追加", "공급자 추가"],
    ["編輯來源", "Edit Provider", "编辑来源", "プロバイダーを編集", "공급자 편집"],
    ["名稱", "Name", "名称", "名前", "이름"],
    ["類型", "Type", "类型", "種類", "유형"],
    ["線上", "Online", "在线", "オンライン", "온라인"],
    ["本地", "Local", "本地", "ローカル", "로컬"],
    ["金鑰", "Key", "密钥", "キー", "키"],
    ["請輸入 API Key", "Enter an API Key", "请输入 API Key", "API キーを入力", "API 키 입력"],
    ["OAuth 登入", "Sign in with OAuth", "OAuth 登录", "OAuth でログイン", "OAuth 로그인"],
    ["若 callback 未自動完成，可貼回 redirect URL 或 code。", "If the callback does not complete automatically, paste the redirect URL or code.", "若 callback 未自动完成，可贴回 redirect URL 或 code。", "callback が自動完了しない場合は、redirect URL または code を貼り付けてください。", "callback이 자동 완료되지 않으면 redirect URL 또는 code를 붙여 넣으세요."],
    ["手動貼回 redirect URL", "Paste redirect URL manually", "手动贴回 redirect URL", "redirect URL を手動で貼り付け", "redirect URL 수동 붙여넣기"],
    ["貼上 callback 後的完整 redirect URL，或直接貼 authorization code", "Paste the full redirect URL after the callback, or paste the authorization code", "粘贴 callback 后的完整 redirect URL，或直接粘贴 authorization code", "callback 後の完全な redirect URL、または authorization code を貼り付け", "callback 이후 전체 redirect URL 또는 authorization code 붙여넣기"],
    ["完成 OAuth", "Complete OAuth", "完成 OAuth", "OAuth を完了", "OAuth 완료"],
    ["檢查 Chat API", "Check Chat API", "检查 Chat API", "Chat API を確認", "Chat API 확인"],
    ["預設模型", "Default Model", "默认模型", "既定モデル", "기본 모델"],
    ["最大併發", "Maximum Concurrency", "最大并发", "最大同時実行数", "최대 동시 처리"],
    ["預設推理程度", "Default Reasoning Effort", "默认推理程度", "既定の推論レベル", "기본 추론 수준"],
    ["預設", "Default", "默认", "既定", "기본값"],
    ["負責項目", "Responsibilities", "负责项目", "担当項目", "담당 항목"],
    ["描述這個 LLM 的責任邊界，例如適合處理的任務、限制與優先使用情境。", "Describe this LLM's responsibility boundaries, such as suitable tasks, limitations, and preferred use cases.", "描述这个 LLM 的责任边界，例如适合处理的任务、限制与优先使用场景。", "この LLM に適したタスク、制約、優先利用場面など、責任範囲を記述します。", "이 LLM에 적합한 작업, 제한 사항, 우선 사용 상황 등 책임 범위를 설명합니다."],
    ["模型能力", "Model Capabilities", "模型能力", "モデル機能", "모델 기능"],
    ["例如：負責一般對話、摘要、翻譯與低延遲請求；不處理高風險決策或需要長上下文推理的任務。", "For example: handles general chat, summarization, translation, and low-latency requests; does not handle high-risk decisions or tasks requiring long-context reasoning.", "例如：负责一般对话、摘要、翻译与低延迟请求；不处理高风险决策或需要长上下文推理的任务。", "例：一般会話、要約、翻訳、低遅延リクエストを担当し、高リスクな判断や長文脈推論が必要なタスクは扱いません。", "예: 일반 대화, 요약, 번역, 저지연 요청을 담당하며 고위험 의사결정이나 긴 문맥 추론이 필요한 작업은 처리하지 않습니다."],
    ["確定", "OK", "确定", "OK", "확인"],
    ["載入 Provider 設定", "Loading Provider Settings", "加载 Provider 设置", "プロバイダー設定を読み込み中", "공급자 설정 불러오는 중"],
    ["正在初始化來源列表與模型資料", "Initializing providers and model data", "正在初始化来源列表与模型数据", "プロバイダー一覧とモデルデータを初期化中", "공급자 목록과 모델 데이터 초기화 중"],
    ["尚未建立來源", "No providers have been created", "尚未创建来源", "プロバイダーがまだありません", "생성된 공급자가 없습니다"],
    ["切換啟用狀態", "Toggle enabled state", "切换启用状态", "有効状態を切り替え", "활성 상태 전환"],
    ["刪除", "Delete", "删除", "削除", "삭제"],
    ["啟用", "Enabled", "启用", "有効", "활성"],
    ["停用", "Disabled", "停用", "無効", "비활성"],
    ["修改", "Edit", "修改", "編集", "수정"],
    ["統計", "Statistics", "统计", "統計", "통계"],
    ["對話", "Chat", "对话", "チャット", "대화"],
    ["影像理解", "Vision", "图像理解", "画像理解", "이미지 이해"],
    ["影像分析", "Image Analysis", "图像分析", "画像分析", "이미지 분석"],
    ["影像生成", "Image Generation", "图像生成", "画像生成", "이미지 생성"],
    ["影像編修", "Image Editing", "图像编辑", "画像編集", "이미지 편집"],
    ["影像變體", "Image Variations", "图像变体", "画像バリエーション", "이미지 변형"],
    ["影片分析", "Video Analysis", "视频分析", "動画分析", "동영상 분석"],
    ["影片生成", "Video Generation", "视频生成", "動画生成", "동영상 생성"],
    ["聲音分析", "Audio Analysis", "声音分析", "音声分析", "오디오 분석"],
    ["音訊轉錄", "Transcription", "音频转录", "音声文字起こし", "음성 전사"],
    ["音訊翻譯", "Audio Translation", "音频翻译", "音声翻訳", "오디오 번역"],
    ["語音生成", "Text to Speech", "语音生成", "音声生成", "음성 생성"],
    ["工具呼叫", "Tool Calling", "工具调用", "ツール呼び出し", "도구 호출"],
    ["長上下文", "Long Context", "长上下文", "長いコンテキスト", "긴 컨텍스트"],
    ["程式碼", "Coding", "代码", "コーディング", "코딩"],
    ["推理", "Reasoning", "推理", "推論", "추론"],
    ["摘要", "Summarization", "摘要", "要約", "요약"],
    ["翻譯", "Translation", "翻译", "翻訳", "번역"],
    ["資料擷取", "Data Extraction", "数据提取", "データ抽出", "데이터 추출"],

    ["基準測試類型", "Benchmark type", "基准测试类型", "ベンチマーク種別", "벤치마크 유형"],
    ["效能基準測試", "Performance Benchmark", "性能基准测试", "性能ベンチマーク", "성능 벤치마크"],
    ["測量單一請求和連續批次處理的推論速度。基準測試前將卸載其他模型。", "Measure inference speed for single requests and continuous batch processing. Other models are unloaded before the benchmark.", "测量单一请求和连续批处理的推理速度。基准测试前将卸载其他模型。", "単一リクエストと連続バッチ処理の推論速度を測定します。開始前に他のモデルをアンロードします。", "단일 요청과 연속 배치 처리의 추론 속도를 측정합니다. 벤치마크 전에 다른 모델을 언로드합니다."],
    ["單一請求測試", "Single-request Test", "单一请求测试", "単一リクエストテスト", "단일 요청 테스트"],
    ["生成長度：128 tokens（固定）", "Generation length: 128 tokens (fixed)", "生成长度：128 tokens（固定）", "生成長：128 tokens（固定）", "생성 길이: 128 tokens(고정)"],
    ["連續批次處理測試", "Continuous Batch Test", "连续批处理测试", "連続バッチ処理テスト", "연속 배치 처리 테스트"],
    ["批次處理測試使用 pp1024 / tg128", "Batch tests use pp1024 / tg128", "批处理测试使用 pp1024 / tg128", "バッチテストは pp1024 / tg128 を使用", "배치 테스트는 pp1024 / tg128 사용"],
    ["開始基準測試", "Start Benchmark", "开始基准测试", "ベンチマークを開始", "벤치마크 시작"],
    ["智慧基準測試", "Intelligence Benchmark", "智能基准测试", "知能ベンチマーク", "지능 벤치마크"],
    ["透過知識、推理、數學和程式碼基準評估模型智慧。基準測試前將卸載所有其他模型。", "Evaluate model intelligence with knowledge, reasoning, math, and coding benchmarks. All other models are unloaded first.", "通过知识、推理、数学和代码基准评估模型智能。基准测试前将卸载所有其他模型。", "知識、推論、数学、コードのベンチマークでモデルの能力を評価します。開始前に他のモデルをすべてアンロードします。", "지식, 추론, 수학 및 코딩 벤치마크로 모델 지능을 평가합니다. 시작 전에 다른 모든 모델을 언로드합니다."],
    ["固定種子取樣確保不同模型使用相同問題進行公平比較。選擇 Full 使用完整資料集。", "Fixed-seed sampling gives each model the same questions for fair comparison. Select Full to use the complete dataset.", "固定种子采样确保不同模型使用相同问题进行公平比较。选择 Full 使用完整数据集。", "固定シードにより、各モデルへ同じ問題を出して公平に比較します。Full では全データセットを使用します。", "고정 시드 샘플링으로 모델마다 같은 문제를 사용해 공정하게 비교합니다. Full을 선택하면 전체 데이터셋을 사용합니다."],
    ["啟用思考", "Enable Reasoning", "启用思考", "思考を有効化", "사고 활성화"],
    ["預設使用模型的「啟用思考」設定。即使停用，也會自動偵測模型是否進行思考。", "Uses the model's reasoning setting by default. Even when disabled, reasoning is detected automatically.", "默认使用模型的“启用思考”设置。即使停用，也会自动检测模型是否进行思考。", "既定ではモデルの「思考を有効化」設定を使用します。無効でも、モデルが思考したか自動検出します。", "기본적으로 모델의 '사고 활성화' 설정을 사용합니다. 비활성화해도 모델의 사고 여부를 자동 감지합니다."],
    ["尚未執行基準測試", "No benchmark has been run", "尚未执行基准测试", "ベンチマークはまだ実行されていません", "벤치마크를 아직 실행하지 않았습니다"],
    ["尚未完成測試", "The test is not complete", "尚未完成测试", "テストはまだ完了していません", "테스트가 아직 완료되지 않았습니다"],
    ["準備中", "Preparing", "准备中", "準備中", "준비 중"],
    ["等待執行", "Waiting to run", "等待执行", "実行待ち", "실행 대기"],
    ["等待採樣", "Waiting for samples", "等待采样", "サンプル待ち", "샘플 대기"],
    ["批次大小", "Batch Size", "批次大小", "バッチサイズ", "배치 크기"],
    ["加速比", "Speedup", "加速比", "高速化率", "가속비"],
    ["端到端延遲", "End-to-end Latency", "端到端延迟", "エンドツーエンド遅延", "종단 간 지연"],
    ["吞吐量", "Throughput", "吞吐量", "スループット", "처리량"],
    ["總吞吐量", "Total Throughput", "总吞吐量", "総スループット", "총 처리량"],
    ["峰值記憶體", "Peak Memory", "峰值内存", "ピークメモリ", "최대 메모리"],
    ["耗時", "Duration", "耗时", "所要時間", "소요 시간"],
    ["正確率", "Accuracy", "正确率", "正解率", "정확도"],
    ["正確 / 題數", "Correct / Total", "正确 / 题数", "正解 / 問題数", "정답 / 문항 수"],
    ["佇列中", "Queued", "队列中", "キュー待ち", "대기열에 있음"],
    ["執行中", "Running", "执行中", "実行中", "실행 중"],
    ["已完成", "Completed", "已完成", "完了", "완료"],
    ["已取消", "Cancelled", "已取消", "キャンセル済み", "취소됨"],
    ["失敗", "Failed", "失败", "失敗", "실패"],
    ["未知", "Unknown", "未知", "不明", "알 수 없음"],

    ["系統資源", "System Resources", "系统资源", "システムリソース", "시스템 리소스"],
    ["主機資源趨勢與 Runtime 狀態", "Host resource trends and runtime status", "主机资源趋势与 Runtime 状态", "ホストリソースの推移と Runtime 状態", "호스트 리소스 추세 및 Runtime 상태"],
    ["重新整理", "Refresh", "刷新", "更新", "새로 고침"],
    ["系統監看頁籤", "System monitor tabs", "系统监控页签", "システムモニターのタブ", "시스템 모니터 탭"],
    ["資源趨勢", "Resource Trends", "资源趋势", "リソース推移", "리소스 추세"],
    ["詳細資訊", "Details", "详细信息", "詳細情報", "상세 정보"],
    ["顯示方式", "View By", "显示方式", "表示単位", "표시 방식"],
    ["日", "Day", "日", "日", "일"],
    ["週", "Week", "周", "週", "주"],
    ["月", "Month", "月", "月", "월"],
    ["日期", "Date", "日期", "日付", "날짜"],
    ["上一個區間", "Previous range", "上一个区间", "前の期間", "이전 구간"],
    ["下一個區間", "Next range", "下一个区间", "次の期間", "다음 구간"],
    ["尚未載入採樣資料", "Sample data has not been loaded", "尚未加载采样数据", "サンプルデータはまだ読み込まれていません", "샘플 데이터를 아직 불러오지 않았습니다"],
    ["記憶體", "Memory", "内存", "メモリ", "메모리"],
    ["磁碟", "Disk", "磁盘", "ディスク", "디스크"],
    ["網路接收", "Network Receive", "网络接收", "ネットワーク受信", "네트워크 수신"],
    ["網路傳送", "Network Send", "网络发送", "ネットワーク送信", "네트워크 송신"],
    ["所選區間最新速率", "Latest rate in selected range", "所选区间最新速率", "選択期間の最新速度", "선택 구간의 최신 속도"],
    ["所選區間平均使用率", "Average usage in selected range", "所选区间平均使用率", "選択期間の平均使用率", "선택 구간의 평균 사용률"],
    ["可辨識 GPU 裝置的平均使用率", "Average usage of detected GPU devices", "可识别 GPU 设备的平均使用率", "検出された GPU デバイスの平均使用率", "감지된 GPU 장치의 평균 사용률"],
    ["所選區間尚無資料", "No data in the selected range", "所选区间暂无数据", "選択期間にデータはありません", "선택 구간에 데이터가 없습니다"],
    ["尚未偵測到 GPU 使用率", "GPU usage has not been detected", "尚未检测到 GPU 使用率", "GPU 使用率はまだ検出されていません", "GPU 사용률이 감지되지 않았습니다"],
    ["網路流量", "Network Traffic", "网络流量", "ネットワークトラフィック", "네트워크 트래픽"],
    ["每秒接收與傳送位元組數", "Bytes received and sent per second", "每秒接收与发送字节数", "1 秒あたりの受信・送信バイト数", "초당 수신 및 송신 바이트"],
    ["接收", "Receive", "接收", "受信", "수신"],
    ["傳送", "Send", "发送", "送信", "송신"],
    ["正在載入系統資訊...", "Loading system information...", "正在加载系统信息...", "システム情報を読み込み中...", "시스템 정보 불러오는 중..."],
    ["系統資訊載入失敗", "Failed to load system information", "系统信息加载失败", "システム情報の読み込みに失敗しました", "시스템 정보 불러오기 실패"],
    ["正在讀取資源採樣資料...", "Reading resource samples...", "正在读取资源采样数据...", "リソースサンプルを読み込み中...", "리소스 샘플 읽는 중..."],
    ["正在取得系統詳細資訊...", "Getting system details...", "正在获取系统详细信息...", "システム詳細情報を取得中...", "시스템 상세 정보 가져오는 중..."],
    ["尚未偵測", "Not detected", "尚未检测", "未検出", "감지되지 않음"],
    ["主機資訊", "Host Information", "主机信息", "ホスト情報", "호스트 정보"],
    ["主機名稱", "Host Name", "主机名称", "ホスト名", "호스트 이름"],
    ["作業系統", "Operating System", "操作系统", "オペレーティングシステム", "운영 체제"],
    ["核心", "Kernel", "内核", "カーネル", "커널"],
    ["架構", "Architecture", "架构", "アーキテクチャ", "아키텍처"],
    ["執行時間", "Uptime", "运行时间", "稼働時間", "가동 시간"],
    ["開機時間", "Boot Time", "开机时间", "起動時刻", "부팅 시간"],
    ["程序數", "Processes", "进程数", "プロセス数", "프로세스 수"],
    ["型號", "Model", "型号", "モデル", "모델"],
    ["廠商", "Vendor", "厂商", "ベンダー", "제조사"],
    ["實體核心", "Physical Cores", "物理核心", "物理コア", "물리 코어"],
    ["邏輯核心", "Logical Cores", "逻辑核心", "論理コア", "논리 코어"],
    ["頻率", "Frequency", "频率", "周波数", "주파수"],
    ["封裝數", "Packages", "封装数", "パッケージ数", "패키지 수"],
    ["實體記憶體", "Physical Memory", "物理内存", "物理メモリ", "물리 메모리"],
    ["使用率", "Usage", "使用率", "使用率", "사용률"],
    ["可用", "Available", "可用", "利用可能", "사용 가능"],
    ["未安裝", "Not installed", "未安装", "未インストール", "설치되지 않음"],
    ["未偵測", "Not detected", "未检测", "未検出", "감지되지 않음"],
    ["裝置", "Device", "设备", "デバイス", "장치"],
    ["掛載點", "Mount Point", "挂载点", "マウントポイント", "마운트 지점"],
    ["格式", "Format", "格式", "形式", "형식"],
    ["使用量", "Usage", "使用量", "使用量", "사용량"],
    ["網路介面", "Network Interfaces", "网络接口", "ネットワークインターフェース", "네트워크 인터페이스"],
    ["位址", "Address", "地址", "アドレス", "주소"],
    ["溫度感測器", "Temperature Sensors", "温度传感器", "温度センサー", "온도 센서"],
    ["感測器", "Sensor", "传感器", "センサー", "센서"],
    ["目前", "Current", "当前", "現在", "현재"],
    ["高溫", "High", "高温", "高温", "고온"],
    ["臨界", "Critical", "临界", "限界", "임계"],
    ["正在載入系統監看資料...", "Loading system monitor data...", "正在加载系统监控数据...", "システムモニターデータを読み込み中...", "시스템 모니터 데이터 불러오는 중..."],

    ["分別核發 Chat 與 MCP 專用金鑰；兩種金鑰不可交叉呼叫。", "Issue dedicated Chat and MCP keys separately; the two key types cannot be used interchangeably.", "分别签发 Chat 与 MCP 专用密钥；两种密钥不可交叉调用。", "Chat と MCP の専用キーを個別に発行します。2 種類のキーは相互利用できません。", "Chat 및 MCP 전용 키를 각각 발급합니다. 두 키 유형은 서로 바꿔 사용할 수 없습니다."],
    ["金鑰類型", "Key type", "密钥类型", "キーの種類", "키 유형"],
    ["Chat 金鑰", "Chat Keys", "Chat 密钥", "Chat キー", "Chat 키"],
    ["MCP 金鑰", "MCP Keys", "MCP 密钥", "MCP キー", "MCP 키"],
    ["登入金鑰", "Login Keys", "登录密钥", "ログインキー", "로그인 키"],
    ["正在載入金鑰資料...", "Loading key data...", "正在加载密钥数据...", "キーデータを読み込み中...", "키 데이터 불러오는 중..."],
    ["Chat API Key 名稱", "Chat API Key Name", "Chat API Key 名称", "Chat API キー名", "Chat API 키 이름"],
    ["MCP Key 名稱", "MCP Key Name", "MCP Key 名称", "MCP キー名", "MCP 키 이름"],
    ["例如：Codex、Claude Desktop", "For example: Codex, Claude Desktop", "例如：Codex、Claude Desktop", "例：Codex、Claude Desktop", "예: Codex, Claude Desktop"],
    ["例如：外部服務、內部測試", "For example: external service, internal test", "例如：外部服务、内部测试", "例：外部サービス、内部テスト", "예: 외부 서비스, 내부 테스트"],
    ["核發 Chat API Key", "Issue Chat API Key", "签发 Chat API Key", "Chat API キーを発行", "Chat API 키 발급"],
    ["核發 MCP Key", "Issue MCP Key", "签发 MCP Key", "MCP キーを発行", "MCP 키 발급"],
    ["新的 API Key 只會顯示一次", "The new API Key is shown only once", "新的 API Key 只会显示一次", "新しい API キーは一度だけ表示されます", "새 API 키는 한 번만 표시됩니다"],
    ["複製 API Key", "Copy API Key", "复制 API Key", "API キーをコピー", "API 키 복사"],
    ["依名稱類型過濾", "Filter by name type", "按名称类型筛选", "名前の種類で絞り込み", "이름 유형으로 필터링"],
    ["全部名稱", "All Names", "全部名称", "すべての名前", "모든 이름"],
    ["上一頁", "Previous Page", "上一页", "前のページ", "이전 페이지"],
    ["下一頁", "Next Page", "下一页", "다음 페이지", "다음 페이지"],
    ["強制路由", "Forced Routing", "强制路由", "強制ルーティング", "강제 라우팅"],
    ["使用次數", "Usage Count", "使用次数", "使用回数", "사용 횟수"],
    ["建立時間", "Created", "创建时间", "作成日時", "생성 시간"],
    ["修改金鑰", "Edit Key", "修改密钥", "キーを編集", "키 수정"],
    ["推理程度", "Reasoning Effort", "推理程度", "推論レベル", "추론 수준"],
    ["AUTO 代表強制回到負載平衡的自動選擇；指定值會覆寫呼叫端送入的 Provider、模型或推理程度。", "AUTO forces load balancing to choose automatically; a specified value overrides the provider, model, or reasoning effort sent by the caller.", "AUTO 代表强制回到负载均衡的自动选择；指定值会覆盖调用端传入的 Provider、模型或推理程度。", "AUTO はロードバランサーの自動選択を強制します。値を指定すると、呼び出し側のプロバイダー、モデル、推論レベルを上書きします。", "AUTO는 부하 분산의 자동 선택을 강제합니다. 값을 지정하면 호출자가 보낸 공급자, 모델 또는 추론 수준을 덮어씁니다."],
    ["確認操作", "Confirm Action", "确认操作", "操作を確認", "작업 확인"],
    ["API Key 使用統計", "API Key Usage Statistics", "API Key 使用统计", "API キー使用統計", "API 키 사용 통계"],
    ["上個月", "Previous Month", "上个月", "前月", "지난달"],
    ["下個月", "Next Month", "下个月", "翌月", "다음 달"],
    ["尚無可顯示的金鑰", "No keys to display", "暂无可显示的密钥", "表示できるキーはありません", "표시할 키가 없습니다"],
    ["不套用 Chat 路由策略", "Chat routing policy does not apply", "不套用 Chat 路由策略", "Chat ルーティングポリシーは適用されません", "Chat 라우팅 정책을 적용하지 않음"],
    ["僅可呼叫 MCP 端點", "MCP endpoint only", "仅可调用 MCP 端点", "MCP エンドポイントのみ呼び出し可能", "MCP 엔드포인트만 호출 가능"],
    ["其他", "Other", "其他", "その他", "기타"],
    ["請先輸入 Chat API Key 名稱。", "Enter a Chat API Key name first.", "请先输入 Chat API Key 名称。", "先に Chat API キー名を入力してください。", "먼저 Chat API 키 이름을 입력하세요."],
    ["請先輸入 MCP Key 名稱。", "Enter an MCP Key name first.", "请先输入 MCP Key 名称。", "先に MCP キー名を入力してください。", "먼저 MCP 키 이름을 입력하세요."],
    ["正在核發金鑰...", "Issuing key...", "正在签发密钥...", "キーを発行中...", "키 발급 중..."],
    ["Chat API Key 已核發，路由策略預設為 AUTO。", "Chat API Key issued. The routing policy defaults to AUTO.", "Chat API Key 已签发，路由策略默认为 AUTO。", "Chat API キーを発行しました。ルーティングポリシーの既定値は AUTO です。", "Chat API 키가 발급되었습니다. 라우팅 정책 기본값은 AUTO입니다."],
    ["MCP Key 已核發，只能用於 MCP 端點。", "MCP Key issued. It can only be used with the MCP endpoint.", "MCP Key 已签发，只能用于 MCP 端点。", "MCP キーを発行しました。MCP エンドポイントでのみ使用できます。", "MCP 키가 발급되었습니다. MCP 엔드포인트에서만 사용할 수 있습니다."],
    ["名稱不可空白。", "Name cannot be blank.", "名称不可为空。", "名前を空欄にはできません。", "이름은 비워 둘 수 없습니다."],
    ["正在儲存金鑰設定...", "Saving key settings...", "正在保存密钥设置...", "キー設定を保存中...", "키 설정 저장 중..."],
    ["金鑰設定已更新。", "Key settings updated.", "密钥设置已更新。", "キー設定を更新しました。", "키 설정이 업데이트되었습니다."],
    ["正在處理...", "Processing...", "正在处理...", "処理中...", "처리 중..."],
    ["停用金鑰", "Disable Key", "停用密钥", "キーを無効化", "키 비활성화"],
    ["啟用金鑰", "Enable Key", "启用密钥", "キーを有効化", "키 활성화"],
    ["刪除金鑰", "Delete Key", "删除密钥", "キーを削除", "키 삭제"],
    ["金鑰已刪除。", "Key deleted.", "密钥已删除。", "キーを削除しました。", "키를 삭제했습니다."],
    ["每日使用次數", "Daily Usage Count", "每日使用次数", "日別使用回数", "일일 사용 횟수"],
    ["每日使用量", "Daily Usage", "每日使用量", "日別使用量", "일일 사용량"],
    ["每日使用量統計", "Daily Usage Statistics", "每日使用量统计", "日別使用量統計", "일일 사용량 통계"],
    ["尚未載入統計資料。", "Statistics have not been loaded.", "尚未加载统计数据。", "統計データはまだ読み込まれていません。", "통계 데이터를 아직 불러오지 않았습니다."],
    ["正在載入統計資料...", "Loading statistics...", "正在加载统计数据...", "統計データを読み込み中...", "통계 데이터 불러오는 중..."],
    ["圖表元件載入失敗", "Failed to load chart component", "图表组件加载失败", "チャートコンポーネントの読み込みに失敗しました", "차트 구성 요소 불러오기 실패"],
    ["API Key 已複製。", "API Key copied.", "API Key 已复制。", "API キーをコピーしました。", "API 키를 복사했습니다."],

    ["登入管理介面", "Sign in to Administration", "登录管理界面", "管理画面にログイン", "관리 화면 로그인"],
    ["帳號", "Account", "账号", "アカウント", "계정"],
    ["密碼", "Password", "密码", "パスワード", "비밀번호"],
    ["登入", "Sign In", "登录", "ログイン", "로그인"],
    ["登入後會建立 24 小時有效的臨時 API Key，並以 Cookie 保存。", "Signing in creates a temporary API Key valid for 24 hours and stores it in a cookie.", "登录后会创建有效期为 24 小时的临时 API Key，并以 Cookie 保存。", "ログインすると 24 時間有効な一時 API キーを作成し、Cookie に保存します。", "로그인하면 24시간 유효한 임시 API 키를 생성하여 쿠키에 저장합니다."],
    ["登入失敗", "Sign-in failed", "登录失败", "ログインに失敗しました", "로그인 실패"],
    ["登入已失效，請重新登入。", "Your session has expired. Sign in again.", "登录已失效，请重新登录。", "ログインの有効期限が切れました。もう一度ログインしてください。", "로그인이 만료되었습니다. 다시 로그인하세요."],
    ["前往主頁面", "Go to the main page", "前往主页面", "メインページへ", "메인 페이지로 이동"]
  ];

  rows.push(
    ["正在重新載入資料...", "Reloading data...", "正在重新加载数据...", "データを再読み込み中...", "데이터 다시 불러오는 중..."],
    ["正在建立切片上傳工作...", "Creating chunked upload...", "正在创建分片上传任务...", "分割アップロードを作成中...", "분할 업로드 작업 생성 중..."],
    ["取消上傳", "Cancel Upload", "取消上传", "アップロードをキャンセル", "업로드 취소"],
    ["後端未回傳更新切片工作識別碼", "The backend did not return an upload session ID", "后端未返回更新分片任务标识符", "バックエンドからアップロードセッション ID が返されませんでした", "백엔드에서 업로드 세션 ID를 반환하지 않았습니다"],
    ["上傳完成，正在驗證並排程更新...", "Upload complete. Validating and scheduling the update...", "上传完成，正在验证并安排更新...", "アップロード完了。検証して更新をスケジュール中...", "업로드 완료. 검증 후 업데이트 예약 중..."],
    ["更新已接收，但缺少操作識別碼，無法驗證更新結果。", "The update was received, but no operation ID was returned, so the result cannot be verified.", "更新已接收，但缺少操作标识符，无法验证更新结果。", "更新は受信されましたが、操作 ID がないため結果を確認できません。", "업데이트를 받았지만 작업 ID가 없어 결과를 확인할 수 없습니다."],
    ["更新已排程，正在等待服務重新啟動...", "Update scheduled. Waiting for the service to restart...", "更新已安排，正在等待服务重新启动...", "更新をスケジュールしました。サービスの再起動を待機中...", "업데이트 예약됨. 서비스 재시작 대기 중..."],
    ["更新中", "Updating", "更新中", "更新中", "업데이트 중"],
    ["更新上傳失敗，服務尚未進入更新程序。", "Update upload failed. The service has not entered the update process.", "更新上传失败，服务尚未进入更新流程。", "更新のアップロードに失敗しました。更新処理は開始されていません。", "업데이트 업로드에 실패했습니다. 서비스가 업데이트 절차에 진입하지 않았습니다."],
    ["偵測到另一筆更新操作，請查看更新紀錄。", "Another update operation was detected. Check the update log.", "检测到另一项更新操作，请查看更新记录。", "別の更新操作が検出されました。更新ログを確認してください。", "다른 업데이트 작업이 감지되었습니다. 업데이트 기록을 확인하세요."],
    ["更新套用失敗，系統已嘗試回滾；請查看 service_update.log。", "Failed to apply the update. The system attempted a rollback; check service_update.log.", "应用更新失败，系统已尝试回滚；请查看 service_update.log。", "更新の適用に失敗し、ロールバックを試みました。service_update.log を確認してください。", "업데이트 적용에 실패하여 롤백을 시도했습니다. service_update.log를 확인하세요."],
    ["服務已重啟，但 website/main.html 與更新包不一致；請查看 service_update.log。", "The service restarted, but website/main.html does not match the update package; check service_update.log.", "服务已重启，但 website/main.html 与更新包不一致；请查看 service_update.log。", "サービスは再起動しましたが、website/main.html が更新パッケージと一致しません。service_update.log を確認してください。", "서비스가 재시작되었지만 website/main.html이 업데이트 패키지와 일치하지 않습니다. service_update.log를 확인하세요."],
    ["等待服務重啟逾時，請稍後手動重新整理頁面，並查看 data/system/service_update/service_update.log。", "Timed out waiting for the service to restart. Refresh the page later and check data/system/service_update/service_update.log.", "等待服务重启超时，请稍后手动刷新页面，并查看 data/system/service_update/service_update.log。", "サービスの再起動待ちがタイムアウトしました。後でページを更新し、data/system/service_update/service_update.log を確認してください。", "서비스 재시작 대기 시간이 초과되었습니다. 나중에 페이지를 새로 고치고 data/system/service_update/service_update.log를 확인하세요."],
    ["正在更新儀表板資料...", "Updating dashboard data...", "正在更新仪表板数据...", "ダッシュボードデータを更新中...", "대시보드 데이터 업데이트 중..."],
    ["對話黏著時間必須是 1 到 10080 分鐘的整數。", "Conversation affinity must be an integer from 1 to 10080 minutes.", "对话粘性时间必须是 1 到 10080 分钟的整数。", "会話アフィニティ時間は 1～10080 分の整数にしてください。", "대화 고정 시간은 1~10080분의 정수여야 합니다."],
    ["配額容忍差必須介於 0 到 100 個百分點。", "Quota tolerance must be between 0 and 100 percentage points.", "配额容忍差必须介于 0 到 100 个百分点。", "クォータ許容差は 0～100 ポイントにしてください。", "할당량 허용 차이는 0~100%p여야 합니다."],
    ["Response 路由上限必須是 100 到 100000 的整數。", "The Response route limit must be an integer from 100 to 100000.", "Response 路由上限必须是 100 到 100000 的整数。", "Response ルート上限は 100～100000 の整数にしてください。", "Response 경로 한도는 100~100000의 정수여야 합니다."],
    ["容量冷卻必須是 1 到 300 秒的整數。", "Capacity cooldown must be an integer from 1 to 300 seconds.", "容量冷却必须是 1 到 300 秒的整数。", "容量クールダウンは 1～300 秒の整数にしてください。", "용량 대기 시간은 1~300초의 정수여야 합니다."],
    ["已設定 API Key，留空表示保留原金鑰", "An API Key is set; leave blank to keep it", "已设置 API Key，留空表示保留原密钥", "API キーは設定済みです。空欄で既存キーを保持します", "API 키가 설정되어 있습니다. 비워 두면 기존 키를 유지합니다"],
    ["選填", "Optional", "选填", "任意", "선택 사항"],
    ["通知目標已儲存。", "Notification target saved.", "通知目标已保存。", "通知先を保存しました。", "알림 대상을 저장했습니다."],
    ["儲存通知目標失敗。", "Failed to save the notification target.", "保存通知目标失败。", "通知先の保存に失敗しました。", "알림 대상 저장에 실패했습니다."],
    ["通知測試已送出。", "Test notification sent.", "测试通知已发送。", "テスト通知を送信しました。", "테스트 알림을 보냈습니다."],
    ["通知測試失敗。", "Test notification failed.", "测试通知失败。", "テスト通知に失敗しました。", "테스트 알림에 실패했습니다."],
    ["金鑰資料已載入。", "Key data loaded.", "密钥数据已加载。", "キーデータを読み込みました。", "키 데이터를 불러왔습니다."],
    ["無法載入金鑰資料。", "Unable to load key data.", "无法加载密钥数据。", "キーデータを読み込めません。", "키 데이터를 불러올 수 없습니다."],
    ["此名稱類型沒有可顯示的 API Key", "No API Keys match this name type", "此名称类型没有可显示的 API Key", "この名前の種類に一致する API キーはありません", "이 이름 유형에 표시할 API 키가 없습니다"],
    ["尚無可顯示的登入金鑰", "No login keys to display", "暂无可显示的登录密钥", "表示できるログインキーはありません", "표시할 로그인 키가 없습니다"],
    ["尚無可顯示的 Chat API Key", "No Chat API Keys to display", "暂无可显示的 Chat API Key", "表示できる Chat API キーはありません", "표시할 Chat API 키가 없습니다"],
    ["未分類", "Uncategorized", "未分类", "未分類", "미분류"],
    ["未命名金鑰", "Unnamed Key", "未命名密钥", "名前のないキー", "이름 없는 키"],
    ["Chat API Key 已核發，只可呼叫模型列表與 Chat Completions。", "Chat API Key issued. It can only call the model list and Chat Completions.", "Chat API Key 已签发，仅可调用模型列表与 Chat Completions。", "Chat API キーを発行しました。モデル一覧と Chat Completions のみ呼び出せます。", "Chat API 키가 발급되었습니다. 모델 목록과 Chat Completions만 호출할 수 있습니다."],
    ["核發 API Key 失敗。", "Failed to issue the API Key.", "签发 API Key 失败。", "API キーの発行に失敗しました。", "API 키 발급에 실패했습니다."],
    ["目前沒有可複製的 API Key。", "There is no API Key to copy.", "目前没有可复制的 API Key。", "コピーできる API キーがありません。", "복사할 API 키가 없습니다."],
    ["API Key 已複製到剪貼簿。", "API Key copied to the clipboard.", "API Key 已复制到剪贴板。", "API キーをクリップボードにコピーしました。", "API 키를 클립보드에 복사했습니다."],
    ["已複製", "Copied", "已复制", "コピー済み", "복사됨"],
    ["無法寫入剪貼簿，請手動複製。", "Unable to write to the clipboard. Copy it manually.", "无法写入剪贴板，请手动复制。", "クリップボードに書き込めません。手動でコピーしてください。", "클립보드에 쓸 수 없습니다. 직접 복사하세요."],
    ["更新 API Key 狀態失敗。", "Failed to update API Key status.", "更新 API Key 状态失败。", "API キー状態の更新に失敗しました。", "API 키 상태 업데이트에 실패했습니다."],
    ["API Key 名稱已更新。", "API Key name updated.", "API Key 名称已更新。", "API キー名を更新しました。", "API 키 이름이 업데이트되었습니다."],
    ["更新 API Key 名稱失敗。", "Failed to update the API Key name.", "更新 API Key 名称失败。", "API キー名の更新に失敗しました。", "API 키 이름 업데이트에 실패했습니다."],
    ["刪除 API Key 失敗。", "Failed to delete the API Key.", "删除 API Key 失败。", "API キーの削除に失敗しました。", "API 키 삭제에 실패했습니다."],
    ["統計資料載入中。", "Loading statistics.", "正在加载统计数据。", "統計データを読み込み中です。", "통계 데이터 불러오는 중입니다."],
    ["無法載入使用統計。", "Unable to load usage statistics.", "无法加载使用统计。", "使用統計を読み込めません。", "사용 통계를 불러올 수 없습니다."],
    ["Chart.js 載入失敗。", "Failed to load Chart.js.", "Chart.js 加载失败。", "Chart.js の読み込みに失敗しました。", "Chart.js 불러오기 실패."],
    ["Chart.js 尚未載入，無法繪製圖表。", "Chart.js is not loaded, so the chart cannot be drawn.", "Chart.js 尚未加载，无法绘制图表。", "Chart.js が読み込まれていないため、チャートを描画できません。", "Chart.js가 로드되지 않아 차트를 그릴 수 없습니다."],
    ["每日用量 = 當日 00:00 起始可使用量 - 最新可使用量", "Daily usage = quota available at 00:00 - latest available quota", "每日用量 = 当日 00:00 起始可用量 - 最新可用量", "日別使用量 = 当日 00:00 の利用可能量 - 最新の利用可能量", "일일 사용량 = 당일 00:00 시작 가용량 - 최신 가용량"],
    ["無法載入 Provider 用量統計。", "Unable to load provider usage statistics.", "无法加载 Provider 用量统计。", "プロバイダー使用量統計を読み込めません。", "공급자 사용량 통계를 불러올 수 없습니다."],
    ["尚無額度紀錄", "No quota records", "暂无额度记录", "クォータ記録はありません", "할당량 기록 없음"],
    ["目前剩餘", "Currently remaining", "目前剩余", "現在の残量", "현재 남음"],
    ["正在取得頁面資訊...", "Getting page information...", "正在获取页面信息...", "ページ情報を取得中...", "페이지 정보 가져오는 중..."],
    ["只接受 ZIP 更新檔。", "Only ZIP update files are accepted.", "仅接受 ZIP 更新文件。", "ZIP 更新ファイルのみ使用できます。", "ZIP 업데이트 파일만 허용됩니다."],
    ["更新檔案不得超過 128 MB。", "The update file cannot exceed 128 MB.", "更新文件不得超过 128 MB。", "更新ファイルは 128 MB 以下にしてください。", "업데이트 파일은 128MB를 초과할 수 없습니다."],
    ["正在重新載入設定...", "Reloading settings...", "正在重新加载设置...", "設定を再読み込み中...", "설정 다시 불러오는 중..."],
    ["正在重新載入金鑰資料...", "Reloading key data...", "正在重新加载密钥数据...", "キーデータを再読み込み中...", "키 데이터 다시 불러오는 중..."],
    ["正在重新載入進階設定...", "Reloading advanced settings...", "正在重新加载高级设置...", "詳細設定を再読み込み中...", "고급 설정 다시 불러오는 중..."],
    ["正在重新載入通知設定...", "Reloading notification settings...", "正在重新加载通知设置...", "通知設定を再読み込み中...", "알림 설정 다시 불러오는 중..."],

    ["此來源未支援 OAuth，請使用 API Key。", "This provider does not support OAuth. Use an API Key.", "此来源不支持 OAuth，请使用 API Key。", "このプロバイダーは OAuth に対応していません。API キーを使用してください。", "이 공급자는 OAuth를 지원하지 않습니다. API 키를 사용하세요."],
    ["OAuth 不適用", "OAuth not available", "OAuth 不适用", "OAuth は利用できません", "OAuth 사용 불가"],
    ["OAuth 連線中...", "Connecting OAuth...", "OAuth 连接中...", "OAuth 接続中...", "OAuth 연결 중..."],
    ["OAuth 已連線", "OAuth connected", "OAuth 已连接", "OAuth 接続済み", "OAuth 연결됨"],
    ["請貼上 redirect URL 或 authorization code。", "Paste the redirect URL or authorization code.", "请粘贴 redirect URL 或 authorization code。", "redirect URL または authorization code を貼り付けてください。", "redirect URL 또는 authorization code를 붙여 넣으세요."],
    ["載入模型...", "Loading models...", "加载模型...", "モデルを読み込み中...", "모델 불러오는 중..."],
    ["模型 API 無法取得", "Model API unavailable", "无法获取模型 API", "モデル API を取得できません", "모델 API를 가져올 수 없음"],
    ["已儲存", "Saved", "已保存", "保存済み", "저장됨"],
    ["目前只有 OpenAI Codex Provider 支援 OAuth。", "Only the OpenAI Codex provider currently supports OAuth.", "目前只有 OpenAI Codex Provider 支持 OAuth。", "現在 OAuth に対応しているのは OpenAI Codex プロバイダーのみです。", "현재 OpenAI Codex 공급자만 OAuth를 지원합니다."],
    ["OAuth 尚未自動完成，請貼上登入完成後的 redirect URL 或 authorization code。", "OAuth did not complete automatically. Paste the redirect URL or authorization code after signing in.", "OAuth 尚未自动完成，请粘贴登录完成后的 redirect URL 或 authorization code。", "OAuth が自動完了しませんでした。ログイン後の redirect URL または authorization code を貼り付けてください。", "OAuth가 자동 완료되지 않았습니다. 로그인 후 redirect URL 또는 authorization code를 붙여 넣으세요."],
    ["OAuth 啟動失敗", "Failed to start OAuth", "OAuth 启动失败", "OAuth の開始に失敗しました", "OAuth 시작 실패"],
    ["OAuth 完成失敗。", "Failed to complete OAuth.", "OAuth 完成失败。", "OAuth の完了に失敗しました。", "OAuth 완료 실패."],
    ["OAuth 登入失敗", "OAuth sign-in failed", "OAuth 登录失败", "OAuth ログインに失敗しました", "OAuth 로그인 실패"],
    ["正在完成 OAuth...", "Completing OAuth...", "正在完成 OAuth...", "OAuth を完了中...", "OAuth 완료 중..."],
    ["OAuth 已連線。", "OAuth connected.", "OAuth 已连接。", "OAuth に接続しました。", "OAuth 연결됨."],

    ["等待測試完成", "Waiting for the test to finish", "等待测试完成", "テストの完了を待機中", "테스트 완료 대기 중"],
    ["建立測試工作", "Create Test Job", "创建测试任务", "テストジョブを作成", "테스트 작업 생성"],
    ["等待後端建立佇列", "Waiting for the backend queue", "等待后端创建队列", "バックエンドのキュー作成を待機中", "백엔드 대기열 생성 대기 중"],
    ["建立測試失敗", "Failed to create the test", "创建测试失败", "テストの作成に失敗しました", "테스트 생성 실패"],
    ["請確認 Provider、模型與 API 連線狀態", "Check the provider, model, and API connection", "请确认 Provider、模型与 API 连接状态", "プロバイダー、モデル、API の接続状態を確認してください", "공급자, 모델 및 API 연결 상태를 확인하세요"],
    ["取得測試狀態失敗", "Failed to get test status", "获取测试状态失败", "テスト状態の取得に失敗しました", "테스트 상태 가져오기 실패"],
    ["取消失敗", "Cancellation failed", "取消失败", "キャンセルに失敗しました", "취소 실패"],
    ["基準測試失敗", "Benchmark failed", "基准测试失败", "ベンチマークに失敗しました", "벤치마크 실패"],
    ["無法載入 Provider", "Unable to load providers", "无法加载 Provider", "プロバイダーを読み込めません", "공급자를 불러올 수 없습니다"],

    ["重新整理詳細資訊", "Refresh details", "刷新详细信息", "詳細情報を更新", "상세 정보 새로 고침"],
    ["Swap 使用率", "Swap Usage", "Swap 使用率", "Swap 使用率", "Swap 사용률"],
    ["GPU 與 Runtime", "GPU and Runtime", "GPU 与 Runtime", "GPU と Runtime", "GPU 및 Runtime"],
    ["GPU 使用率", "GPU Usage", "GPU 使用率", "GPU 使用率", "GPU 사용률"],
    ["GPU 來源", "GPU Source", "GPU 来源", "GPU ソース", "GPU 소스"],

    ["正在載入金鑰與路由選項...", "Loading keys and routing options...", "正在加载密钥与路由选项...", "キーとルーティング選択肢を読み込み中...", "키 및 라우팅 옵션 불러오는 중..."],
    ["登入金鑰只供 Web Session 使用，不套用 Chat 路由策略。", "Login keys are only for web sessions; the Chat routing policy does not apply.", "登录密钥仅供 Web Session 使用，不套用 Chat 路由策略。", "ログインキーは Web Session 専用で、Chat ルーティングポリシーは適用されません。", "로그인 키는 Web Session 전용이며 Chat 라우팅 정책을 적용하지 않습니다."],
    ["MCP 金鑰只供 MCP 端點使用，不套用 Chat 路由策略，也不收集使用統計。", "MCP keys are only for the MCP endpoint. Chat routing does not apply, and usage statistics are not collected.", "MCP 密钥仅供 MCP 端点使用，不套用 Chat 路由策略，也不收集使用统计。", "MCP キーは MCP エンドポイント専用です。Chat ルーティングは適用されず、使用統計も収集しません。", "MCP 키는 MCP 엔드포인트 전용입니다. Chat 라우팅이 적용되지 않으며 사용 통계도 수집하지 않습니다."],
    ["AUTO 不覆寫呼叫端設定；未指定時由負載平衡自動選擇。指定值則會強制覆寫呼叫端送入的 Provider、模型或推理程度。", "AUTO does not override caller settings; when unspecified, load balancing chooses automatically. A specified value forces an override of the caller's provider, model, or reasoning effort.", "AUTO 不覆盖调用端设置；未指定时由负载均衡自动选择。指定值则会强制覆盖调用端传入的 Provider、模型或推理程度。", "AUTO は呼び出し側の設定を上書きせず、未指定の場合はロードバランサーが自動選択します。値を指定すると呼び出し側のプロバイダー、モデル、推論レベルを強制的に上書きします。", "AUTO는 호출자 설정을 덮어쓰지 않으며 미지정 시 부하 분산이 자동 선택합니다. 값을 지정하면 호출자의 공급자, 모델 또는 추론 수준을 강제로 덮어씁니다."],

    ["模型", "Model", "模型", "モデル", "모델"],
    ["全部", "All", "全部", "すべて", "전체"],
    ["上一個月", "Previous Month", "上一个月", "前月", "지난달"],
    ["下一個月", "Next Month", "下一个月", "翌月", "다음 달"],
    ["啟用中", "Enabled", "启用中", "有効", "활성"],
    ["停用中", "Disabled", "停用中", "無効", "비활성"],
    ["尚未載入金鑰資料。", "Key data has not been loaded.", "尚未加载密钥数据。", "キーデータはまだ読み込まれていません。", "키 데이터를 아직 불러오지 않았습니다."],
    ["確定要將儀表板累計數值歸零嗎？原始歷史紀錄仍會保留。", "Reset the cumulative dashboard values? Raw history will be preserved.", "确定要将仪表板累计数值归零吗？原始历史记录仍会保留。", "ダッシュボードの累計値をリセットしますか？元の履歴は保持されます。", "대시보드 누적 값을 초기화할까요? 원본 기록은 유지됩니다."],
    ["確定要刪除此 API Key 嗎？刪除後無法復原。", "Delete this API Key? This action cannot be undone.", "确定要删除此 API Key 吗？删除后无法恢复。", "この API キーを削除しますか？この操作は元に戻せません。", "이 API 키를 삭제할까요? 삭제 후 되돌릴 수 없습니다."],
    ["資源監看", "Resource Monitor", "资源监控", "リソースモニター", "리소스 모니터"],
    ["系統詳細資訊", "System Details", "系统详细信息", "システム詳細情報", "시스템 상세 정보"],
    ["名稱類型", "Name Type", "名称类型", "名前の種類", "이름 유형"],
    ["確認刪除 API Key", "Confirm API Key Deletion", "确认删除 API Key", "API キー削除の確認", "API 키 삭제 확인"],
    ["修改 API Key 名稱", "Edit API Key Name", "修改 API Key 名称", "API キー名を編集", "API 키 이름 수정"],
    ["。", ".", "。", "。", "."],
    ["五小時", "5 hours", "五小时", "5時間", "5시간"],
    ["7日", "7 days", "7日", "7日", "7일"],
    ["23:59 剩餘", "Remaining at 23:59", "23:59 剩余", "23:59 時点の残量", "23:59 기준 잔여량"],
    ["目前剩餘", "Currently Remaining", "目前剩余", "現在の残量", "현재 잔여량"],
    ["（已停用）", " (disabled)", "（已停用）", "（無効）", " (비활성)"],
    ["支援 OpenAI /v1/responses 相容 API", "Supports the OpenAI-compatible /v1/responses API", "支持 OpenAI /v1/responses 兼容 API", "OpenAI 互換 /v1/responses API に対応", "OpenAI 호환 /v1/responses API 지원"],
    ["這是 OAuth 登入起始連結，不是登入完成後的 callback URL。請先完成瀏覽器登入，再貼回網址列中的 redirect URL，或只貼 code 參數。", "This is the OAuth sign-in URL, not the callback URL after sign-in. Complete sign-in in the browser, then paste the redirect URL from the address bar or only the code parameter.", "这是 OAuth 登录起始链接，不是登录完成后的 callback URL。请先完成浏览器登录，再粘贴地址栏中的 redirect URL，或只粘贴 code 参数。", "これは OAuth ログイン開始 URL で、ログイン後の callback URL ではありません。ブラウザーでログインを完了し、アドレスバーの redirect URL または code パラメーターのみを貼り付けてください。", "이 주소는 OAuth 로그인 시작 URL이며 로그인 완료 후 callback URL이 아닙니다. 브라우저 로그인을 완료한 뒤 주소 표시줄의 redirect URL 또는 code 매개변수만 붙여 넣으세요."],
    ["貼上的 URL 沒有 code 參數。請貼登入完成後的 redirect URL，或直接貼 authorization code。", "The pasted URL has no code parameter. Paste the redirect URL after sign-in or paste the authorization code directly.", "粘贴的 URL 没有 code 参数。请粘贴登录完成后的 redirect URL，或直接粘贴 authorization code。", "貼り付けた URL に code パラメーターがありません。ログイン後の redirect URL または authorization code を貼り付けてください。", "붙여 넣은 URL에 code 매개변수가 없습니다. 로그인 완료 후 redirect URL 또는 authorization code를 붙여 넣으세요."],

    ["執行基準測試", "Run Benchmark", "执行基准测试", "ベンチマークを実行", "벤치마크 실행"],
    ["單一請求結果", "Single-request Results", "单一请求结果", "単一リクエスト結果", "단일 요청 결과"],
    ["併行處理", "Parallel Processing", "并行处理", "並列処理", "병렬 처리"],
    ["尚未執行批次測試", "No batch test has been run", "尚未执行批次测试", "バッチテストはまだ実行されていません", "배치 테스트를 아직 실행하지 않았습니다"],
    ["指標參考", "Metric Reference", "指标参考", "指標リファレンス", "지표 참고"],
    ["（首個 Token 生成時間）", "(Time to first token)", "（首个 Token 生成时间）", "（最初のトークン生成時間）", "(첫 토큰 생성 시간)"],
    ["（Token 生成 TPS）", "(Token generation TPS)", "（Token 生成 TPS）", "（トークン生成 TPS）", "(토큰 생성 TPS)"],
    ["模型開始回應前的延遲。衡量 Prompt 處理速度，越低越好。", "Latency before the model begins responding. It measures prompt processing speed; lower is better.", "模型开始响应前的延迟。衡量 Prompt 处理速度，越低越好。", "モデルが応答を開始するまでの遅延です。Prompt 処理速度を示し、低いほど優れています。", "모델이 응답을 시작하기 전의 지연입니다. Prompt 처리 속도를 나타내며 낮을수록 좋습니다."],
    ["（Prompt 處理 TPS）", "(Prompt processing TPS)", "（Prompt 处理 TPS）", "（Prompt 処理 TPS）", "(Prompt 처리 TPS)"],
    ["預填充階段每秒處理的輸入 Token 數，越高越好。", "Input tokens processed per second during prefill; higher is better.", "预填充阶段每秒处理的输入 Token 数，越高越好。", "プリフィル段階で 1 秒あたりに処理する入力トークン数です。高いほど優れています。", "프리필 단계에서 초당 처리하는 입력 토큰 수이며 높을수록 좋습니다."],
    ["（每個 Token 的耗時）", "(Time per token)", "（每个 Token 的耗时）", "（トークンごとの所要時間）", "(토큰당 소요 시간)"],
    ["每個 Token 生成之間的平均耗時，越低越好。", "Average time between generated tokens; lower is better.", "每个 Token 生成之间的平均耗时，越低越好。", "トークン生成間の平均時間です。低いほど優れています。", "토큰 생성 사이의 평균 시간이며 낮을수록 좋습니다."],
    ["從提交請求到完整回應的總時間，包含預填充與生成。", "Total time from request submission to the complete response, including prefill and generation.", "从提交请求到完整响应的总时间，包含预填充与生成。", "リクエスト送信から完全な応答までの合計時間で、プリフィルと生成を含みます。", "요청 제출부터 전체 응답까지의 총 시간으로 프리필과 생성을 포함합니다."],
    ["每秒生成的 Token 數，為 TPOT 的倒數，越高越好。", "Tokens generated per second, the inverse of TPOT; higher is better.", "每秒生成的 Token 数，为 TPOT 的倒数，越高越好。", "1 秒あたりの生成トークン数で、TPOT の逆数です。高いほど優れています。", "초당 생성 토큰 수로 TPOT의 역수이며 높을수록 좋습니다."],
    ["每秒處理的 Token 總數（輸入 + 輸出），衡量整體使用率。", "Total input and output tokens processed per second, measuring overall utilization.", "每秒处理的 Token 总数（输入 + 输出），衡量整体使用率。", "1 秒あたりに処理する入力・出力トークンの合計で、全体の使用率を示します。", "초당 처리하는 입력 및 출력 토큰 총수로 전체 사용률을 나타냅니다."],
    ["同時處理的並發請求數。批次越大總吞吐量越高，但單個請求延遲會增加。", "Number of concurrent requests processed together. Larger batches improve total throughput but increase per-request latency.", "同时处理的并发请求数。批次越大总吞吐量越高，但单个请求延迟会增加。", "同時に処理するリクエスト数です。バッチが大きいほど総スループットは上がりますが、個々の遅延も増えます。", "동시에 처리하는 요청 수입니다. 배치가 클수록 총 처리량은 높아지지만 개별 요청 지연도 증가합니다."],
    ["相對於單一請求基準（1x）的 Token 生成吞吐量倍數，越高越好。", "Token generation throughput relative to the single-request baseline (1x); higher is better.", "相对于单一请求基准（1x）的 Token 生成吞吐量倍数，越高越好。", "単一リクエスト基準（1x）に対するトークン生成スループット倍率です。高いほど優れています。", "단일 요청 기준(1x) 대비 토큰 생성 처리량 배수이며 높을수록 좋습니다."],
    ["同時處理多個問題。批次越大速度越快，但佔用更多記憶體。", "Process multiple questions at once. Larger batches are faster but use more memory.", "同时处理多个问题。批次越大速度越快，但占用更多内存。", "複数の問題を同時処理します。バッチが大きいほど高速ですが、メモリ使用量も増えます。", "여러 문제를 동시에 처리합니다. 배치가 클수록 빠르지만 더 많은 메모리를 사용합니다."],
    ["執行", "Run", "执行", "実行", "실행"],
    ["智慧測試結果", "Intelligence Test Results", "智能测试结果", "知能テスト結果", "지능 테스트 결과"]
  );

  const localeIndex = { en: 1, "zh-CN": 2, ja: 3, ko: 4 };
  const catalogs = {};
  Object.keys(localeIndex).forEach((name) => {
    catalogs[name] = new Map(rows.map((row) => [row[0], row[localeIndex[name]]]));
  });

  // Benchmark catalog data is supplied by the backend in English (with three localized dataset descriptions).
  // Keep these aliases separate so the normal Traditional-Chinese source catalog remains unambiguous.
  const alternateRows = [
    ["COMMONSENSE & REASONING", "常識與推理", "常识与推理", "COMMONSENSE & REASONING", "常識と推論", "상식 및 추론"],
    ["MATH", "數學", "数学", "MATH", "数学", "수학"],
    ["CODING", "程式碼", "代码", "CODING", "コーディング", "코딩"],
    ["SAFETY & ALIGNMENT", "安全與對齊", "安全与对齐", "SAFETY & ALIGNMENT", "安全性とアラインメント", "안전 및 정렬"],
    ["KNOWLEDGE", "知識", "知识", "KNOWLEDGE", "知識", "지식"],
    ["Commonsense reasoning", "常識推理", "常识推理", "Commonsense reasoning", "常識推論", "상식 추론"],
    ["Science reasoning", "科學推理", "科学推理", "Science reasoning", "科学推論", "과학 추론"],
    ["Coreference resolution", "共指解析", "共指解析", "Coreference resolution", "共参照解析", "상호참조 해결"],
    ["Truthfulness", "真實性", "真实性", "Truthfulness", "真実性", "진실성"],
    ["Math reasoning", "數學推理", "数学推理", "Math reasoning", "数学推論", "수학 추론"],
    ["Quantitative reasoning・5-way", "量化推理・5 選項", "量化推理・5 选项", "Quantitative reasoning · 5-way", "定量推論・5択", "정량 추론 · 5지선다"],
    ["Function completion", "函式補全", "函数补全", "Function completion", "関数補完", "함수 완성"],
    ["Python problems", "Python 題目", "Python 题目", "Python problems", "Python 問題", "Python 문제"],
    ["Code generation", "程式碼生成", "代码生成", "Code generation", "コード生成", "코드 생성"],
    ["Social bias・11 categories", "社會偏見・11 類別", "社会偏见・11 类别", "Social bias · 11 categories", "社会的バイアス・11分類", "사회적 편향 · 11개 범주"],
    ["Safety・7 categories", "安全性・7 類別", "安全性・7 类别", "Safety · 7 categories", "安全性・7分類", "안전성 · 7개 범주"],
    ["Knowledge・57 subjects", "知識・57 科目", "知识・57 科目", "Knowledge · 57 subjects", "知識・57科目", "지식 · 57개 과목"],
    ["Hard knowledge・14 subjects (10-way)", "高難度知識・14 科目（10 選項）", "高难度知识・14 科目（10 选项）", "Hard knowledge · 14 subjects (10-way)", "高難度知識・14科目（10択）", "고난도 지식 · 14개 과목(10지선다)"],
    ["한국어 지식・45 과목", "韓文知識・45 科目", "韩文知识・45 科目", "Korean knowledge · 45 subjects", "韓国語知識・45科目", "한국어 지식 · 45개 과목"],
    ["中文知識・67 科目", "中文知識・67 科目", "中文知识・67 科目", "Chinese knowledge · 67 subjects", "中国語知識・67科目", "중국어 지식 · 67개 과목"],
    ["日本語知識・112 科目", "日文知識・112 科目", "日文知识・112 科目", "Japanese knowledge · 112 subjects", "日本語知識・112科目", "일본어 지식 · 112개 과목"]
  ];
  const alternateLocaleIndex = { "zh-TW": 1, "zh-CN": 2, en: 3, ja: 4, ko: 5 };
  const alternateCatalogs = {};
  Object.keys(alternateLocaleIndex).forEach((name) => {
    alternateCatalogs[name] = new Map(alternateRows.map((row) => [row[0], row[alternateLocaleIndex[name]]]));
  });

  const simplifiedCharacterPairs = "儀仪 錶表 鑰钥 設设 進进 階阶 啟启 總总 數数 計计 應应 餘余 載载 顯显 關关 閉闭 開开 會会 負负 選选 擇择 資资 訊讯 動动 對对 話话 黏粘 著着 與与 時时 間间 鐘钟 額额 點点 卻却 遺遗 過过 舊旧 後后 儲储 並并 寫写 測测 試试 統统 驗验 證证 備备 現现 務务 歷历 蓋盖 檔档 確确 認认 刪删 讀读 採采 樣样 詳详 細细 監监 機机 趨趋 勢势 狀状 態态 週周 記记 憶忆 體体 碟碟 網网 傳传 組组 偵侦 裝装 敗败 執执 邏逻 輯辑 頻频 實实 掛挂 溫温 臨临 發发 稱称 類类 濾滤 頁页 強强 寫写 無无 復复 複复 製制 帳账 號号 碼码 臨临 簡简 語语 瀏浏 覽览 業业 斷断 預预 併并 責责 項项 適适 處处 優优 編编 變变 聲声 轉转 錄录 譯译 長长 擷撷 準准 單单 連连 續续 論论 將将 識识 學学 評评 較较 題题 佇伫";
  const charMap = new Map(simplifiedCharacterPairs.split(" ").map((pair) => Array.from(pair)));
  function normalizePreference(value) {
    const normalized = String(value || "auto").trim();
    return SUPPORTED_PREFERENCES.has(normalized) ? normalized : "auto";
  }

  function resolveLocale(value) {
    if (value !== "auto") return value;
    const browserLanguage = String(navigator.languages?.[0] || navigator.language || "en").replace("_", "-").toLowerCase();
    if (browserLanguage.startsWith("zh")) {
      return /(?:hant|tw|hk|mo)/.test(browserLanguage) ? "zh-TW" : "zh-CN";
    }
    if (browserLanguage.startsWith("ja")) return "ja";
    if (browserLanguage.startsWith("ko")) return "ko";
    return "en";
  }

  function simplify(text) {
    return Array.from(text).map((char) => charMap.get(char) || char).join("");
  }

  function translateDynamic(source, targetLocale) {
    const rules = {
      "zh-CN": [
        [/^(\d{4}) 年 (\d{2}) 月$/, (_, year, month) => `${year} 年 ${month} 月`],
        [/^(\d+) 分 (\d+) 秒$/, (_, minutes, seconds) => `${minutes} 分 ${seconds} 秒`],
        [/^(\d+) 天 (\d+) 小時 (\d+) 分鐘$/, (_, days, hours, minutes) => `${days} 天 ${hours} 小时 ${minutes} 分钟`],
        [/^(\d+) 個裝置$/, (_, count) => `${count} 个设备`],
        [/^採樣 (.+)$/, (_, value) => `采样 ${value}`],
        [/^第 (\d+) \/ (\d+) 頁$/, (_, page, total) => `第 ${page} / ${total} 页`],
        [/^(.+) 累計 (.+) 次。$/, (_, month, count) => `${month} 累计 ${count} 次。`],
        [/^確定要將 (.+) 的儀表板累計數值歸零嗎？原始歷史紀錄仍會保留。$/, (_, scope) => `确定要将 ${scope} 的仪表板累计数值归零吗？原始历史记录仍会保留。`],
        [/^確定要刪除「(.+)」嗎？刪除後無法復原，使用此金鑰的請求會立即失效。$/, (_, name) => `确定要删除「${name}」吗？删除后无法恢复，使用此密钥的请求会立即失效。`],
        [/^確定要(停用|啟用)「(.+)」嗎？$/, (_, action, name) => `确定要${action === "停用" ? "停用" : "启用"}「${name}」吗？`],
        [/^確定要永久刪除「(.+)」嗎？此操作無法復原。$/, (_, name) => `确定要永久删除「${name}」吗？此操作无法恢复。`],
        [/^金鑰已(停用|啟用)。$/, (_, action) => `密钥已${action === "停用" ? "停用" : "启用"}。`],
        [/^(.+) · 每日使用次數$/, (_, name) => `${name} · 每日使用次数`],
        [/^正在上傳更新檔案\.\.\. (\d+)%$/, (_, percent) => `正在上传更新文件... ${percent}%`],
        [/^服務已更新為 (.+)，正在重新載入\.\.\.$/, (_, version) => `服务已更新为 ${version}，正在重新加载...`],
        [/^(累積請求|請求數) (.+) · 綁定數 (.+) · 活躍數 (.+) \/ (.+)$/, (_, label, requests, bound, active, capacity) => `${label === "累積請求" ? "累计请求" : "请求数"} ${requests} · 绑定数 ${bound} · 活跃数 ${active} / ${capacity}`],
        [/^(.+)（已停用）$/, (_, value) => `${value}（已停用）`],
        [/^(\d+)日$/, (_, days) => `${days}日`],
        [/^(\d+)小時$/, (_, hours) => `${hours}小时`],
        [/^(\d+)天(\d+)小時(\d+)分鐘後(?:重置)?$/, (_, days, hours, minutes) => `${days}天${hours}小时${minutes}分钟后重置`],
        [/^剩餘 (.+)，(\d+)天(\d+)小時(\d+)分鐘後重置$/, (_, remaining, days, hours, minutes) => `剩余 ${remaining}，${days}天${hours}小时${minutes}分钟后重置`],
        [/^(.+) · (\d+) 筆原始採樣 · (\d+) 秒統計區間 · 保留 (\d+) 天$/, (_, range, samples, seconds, days) => `${range} · ${samples} 笔原始采样 · ${seconds} 秒统计区间 · 保留 ${days} 天`],
        [/^剩餘 (.+) \/ (.+)（(.+)）$/, (_, remaining, limit, percent) => `剩余 ${remaining} / ${limit}（${percent}）`],
        [/^剩餘 (.+) \/ (.+)$/, (_, remaining, limit) => `剩余 ${remaining} / ${limit}`],
        [/^剩餘 (.+)$/, (_, remaining) => `剩余 ${remaining}`]
      ],
      en: [
        [/^(\d{4}) 年 (\d{2}) 月$/, (_, year, month) => `${year}-${month}`],
        [/^(\d+) 分 (\d+) 秒$/, (_, minutes, seconds) => `${minutes}m ${seconds}s`],
        [/^(\d+) 天 (\d+) 小時 (\d+) 分鐘$/, (_, days, hours, minutes) => `${days}d ${hours}h ${minutes}m`],
        [/^(\d+) 個裝置$/, (_, count) => `${count} device${count === "1" ? "" : "s"}`],
        [/^採樣 (.+)$/, (_, value) => `Sampled ${value}`],
        [/^第 (\d+) \/ (\d+) 頁$/, (_, page, total) => `Page ${page} / ${total}`],
        [/^(.+) 累計 (.+) 次。$/, (_, month, count) => `${month}: ${count} total uses.`],
        [/^確定要將 (.+) 的儀表板累計數值歸零嗎？原始歷史紀錄仍會保留。$/, (_, scope) => `Reset cumulative dashboard values for ${scope.replace(/^「(.+)」$/, "“$1”")}? Raw history will be preserved.`],
        [/^確定要刪除「(.+)」嗎？刪除後無法復原，使用此金鑰的請求會立即失效。$/, (_, name) => `Delete “${name}”? This cannot be undone, and requests using this key will fail immediately.`],
        [/^確定要(停用|啟用)「(.+)」嗎？$/, (_, action, name) => `${action === "停用" ? "Disable" : "Enable"} “${name}”?`],
        [/^確定要永久刪除「(.+)」嗎？此操作無法復原。$/, (_, name) => `Permanently delete “${name}”? This action cannot be undone.`],
        [/^金鑰已(停用|啟用)。$/, (_, action) => `Key ${action === "停用" ? "disabled" : "enabled"}.`],
        [/^(.+) · 每日使用次數$/, (_, name) => `${name} · Daily usage count`],
        [/^正在上傳更新檔案\.\.\. (\d+)%$/, (_, percent) => `Uploading update... ${percent}%`],
        [/^服務已更新為 (.+)，正在重新載入\.\.\.$/, (_, version) => `Service updated to ${version}. Reloading...`],
        [/^(累積請求|請求數) (.+) · 綁定數 (.+) · 活躍數 (.+) \/ (.+)$/, (_, label, requests, bound, active, capacity) => `${label === "累積請求" ? "Cumulative requests" : "Requests"} ${requests} · Bound ${bound} · Active ${active} / ${capacity}`],
        [/^(.+)（已停用）$/, (_, value) => `${value} (disabled)`],
        [/^(\d+)日$/, (_, days) => `${days} days`],
        [/^(\d+)小時$/, (_, hours) => `${hours} hours`],
        [/^(\d+)天(\d+)小時(\d+)分鐘後(?:重置)?$/, (_, days, hours, minutes) => `resets in ${days}d ${hours}h ${minutes}m`],
        [/^剩餘 (.+)，(\d+)天(\d+)小時(\d+)分鐘後重置$/, (_, remaining, days, hours, minutes) => `${remaining} remaining, resets in ${days}d ${hours}h ${minutes}m`],
        [/^(.+) · (\d+) 筆原始採樣 · (\d+) 秒統計區間 · 保留 (\d+) 天$/, (_, range, samples, seconds, days) => `${range} · ${samples} raw samples · ${seconds}s interval · retained ${days} days`],
        [/^剩餘 (.+) \/ (.+)（(.+)）$/, (_, remaining, limit, percent) => `${remaining} / ${limit} remaining (${percent})`],
        [/^剩餘 (.+) \/ (.+)$/, (_, remaining, limit) => `${remaining} / ${limit} remaining`],
        [/^剩餘 (.+)$/, (_, remaining) => `${remaining} remaining`]
      ],
      ja: [
        [/^(\d{4}) 年 (\d{2}) 月$/, (_, year, month) => `${year}年${month}月`],
        [/^(\d+) 分 (\d+) 秒$/, (_, minutes, seconds) => `${minutes}分 ${seconds}秒`],
        [/^(\d+) 天 (\d+) 小時 (\d+) 分鐘$/, (_, days, hours, minutes) => `${days}日 ${hours}時間 ${minutes}分`],
        [/^(\d+) 個裝置$/, (_, count) => `${count}台のデバイス`],
        [/^採樣 (.+)$/, (_, value) => `サンプル ${value}`],
        [/^第 (\d+) \/ (\d+) 頁$/, (_, page, total) => `${page} / ${total} ページ`],
        [/^(.+) 累計 (.+) 次。$/, (_, month, count) => `${month} 合計 ${count} 回。`],
        [/^確定要將 (.+) 的儀表板累計數值歸零嗎？原始歷史紀錄仍會保留。$/, (_, scope) => `${scope} のダッシュボード累計値をリセットしますか？元の履歴は保持されます。`],
        [/^確定要刪除「(.+)」嗎？刪除後無法復原，使用此金鑰的請求會立即失效。$/, (_, name) => `「${name}」を削除しますか？元に戻せず、このキーを使うリクエストは直ちに無効になります。`],
        [/^確定要(停用|啟用)「(.+)」嗎？$/, (_, action, name) => `「${name}」を${action === "停用" ? "無効" : "有効"}にしますか？`],
        [/^確定要永久刪除「(.+)」嗎？此操作無法復原。$/, (_, name) => `「${name}」を完全に削除しますか？この操作は元に戻せません。`],
        [/^金鑰已(停用|啟用)。$/, (_, action) => `キーを${action === "停用" ? "無効" : "有効"}にしました。`],
        [/^(.+) · 每日使用次數$/, (_, name) => `${name} · 日別使用回数`],
        [/^正在上傳更新檔案\.\.\. (\d+)%$/, (_, percent) => `更新ファイルをアップロード中... ${percent}%`],
        [/^服務已更新為 (.+)，正在重新載入\.\.\.$/, (_, version) => `サービスを ${version} に更新しました。再読み込み中...`],
        [/^(累積請求|請求數) (.+) · 綁定數 (.+) · 活躍數 (.+) \/ (.+)$/, (_, label, requests, bound, active, capacity) => `${label === "累積請求" ? "累計リクエスト" : "リクエスト"} ${requests} · バインド ${bound} · アクティブ ${active} / ${capacity}`],
        [/^(.+)（已停用）$/, (_, value) => `${value}（無効）`],
        [/^(\d+)日$/, (_, days) => `${days}日`],
        [/^(\d+)小時$/, (_, hours) => `${hours}時間`],
        [/^(\d+)天(\d+)小時(\d+)分鐘後(?:重置)?$/, (_, days, hours, minutes) => `${days}日${hours}時間${minutes}分後にリセット`],
        [/^剩餘 (.+)，(\d+)天(\d+)小時(\d+)分鐘後重置$/, (_, remaining, days, hours, minutes) => `残り ${remaining}、${days}日${hours}時間${minutes}分後にリセット`],
        [/^(.+) · (\d+) 筆原始採樣 · (\d+) 秒統計區間 · 保留 (\d+) 天$/, (_, range, samples, seconds, days) => `${range} · ${samples}件の生サンプル · ${seconds}秒間隔 · ${days}日間保持`],
        [/^剩餘 (.+)$/, (_, remaining) => `残り ${remaining}`]
      ],
      ko: [
        [/^(\d{4}) 年 (\d{2}) 月$/, (_, year, month) => `${year}년 ${month}월`],
        [/^(\d+) 分 (\d+) 秒$/, (_, minutes, seconds) => `${minutes}분 ${seconds}초`],
        [/^(\d+) 天 (\d+) 小時 (\d+) 分鐘$/, (_, days, hours, minutes) => `${days}일 ${hours}시간 ${minutes}분`],
        [/^(\d+) 個裝置$/, (_, count) => `장치 ${count}개`],
        [/^採樣 (.+)$/, (_, value) => `샘플 ${value}`],
        [/^第 (\d+) \/ (\d+) 頁$/, (_, page, total) => `${page} / ${total}페이지`],
        [/^(.+) 累計 (.+) 次。$/, (_, month, count) => `${month} 누적 ${count}회.`],
        [/^確定要將 (.+) 的儀表板累計數值歸零嗎？原始歷史紀錄仍會保留。$/, (_, scope) => `${scope.replace(/^「(.+)」$/, "“$1”")}의 대시보드 누적 값을 초기화할까요? 원본 기록은 유지됩니다.`],
        [/^確定要刪除「(.+)」嗎？刪除後無法復原，使用此金鑰的請求會立即失效。$/, (_, name) => `“${name}”을(를) 삭제할까요? 되돌릴 수 없으며 이 키를 사용하는 요청은 즉시 실패합니다.`],
        [/^確定要(停用|啟用)「(.+)」嗎？$/, (_, action, name) => `“${name}”을(를) ${action === "停用" ? "비활성화" : "활성화"}할까요?`],
        [/^確定要永久刪除「(.+)」嗎？此操作無法復原。$/, (_, name) => `“${name}”을(를) 영구 삭제할까요? 이 작업은 되돌릴 수 없습니다.`],
        [/^金鑰已(停用|啟用)。$/, (_, action) => `키가 ${action === "停用" ? "비활성화" : "활성화"}되었습니다.`],
        [/^(.+) · 每日使用次數$/, (_, name) => `${name} · 일일 사용 횟수`],
        [/^正在上傳更新檔案\.\.\. (\d+)%$/, (_, percent) => `업데이트 파일 업로드 중... ${percent}%`],
        [/^服務已更新為 (.+)，正在重新載入\.\.\.$/, (_, version) => `서비스가 ${version}(으)로 업데이트되었습니다. 다시 불러오는 중...`],
        [/^(累積請求|請求數) (.+) · 綁定數 (.+) · 活躍數 (.+) \/ (.+)$/, (_, label, requests, bound, active, capacity) => `${label === "累積請求" ? "누적 요청" : "요청"} ${requests} · 바인딩 ${bound} · 활성 ${active} / ${capacity}`],
        [/^(.+)（已停用）$/, (_, value) => `${value} (비활성)`],
        [/^(\d+)日$/, (_, days) => `${days}일`],
        [/^(\d+)小時$/, (_, hours) => `${hours}시간`],
        [/^(\d+)天(\d+)小時(\d+)分鐘後(?:重置)?$/, (_, days, hours, minutes) => `${days}일 ${hours}시간 ${minutes}분 후 초기화`],
        [/^剩餘 (.+)，(\d+)天(\d+)小時(\d+)分鐘後重置$/, (_, remaining, days, hours, minutes) => `${remaining} 남음, ${days}일 ${hours}시간 ${minutes}분 후 초기화`],
        [/^(.+) · (\d+) 筆原始採樣 · (\d+) 秒統計區間 · 保留 (\d+) 天$/, (_, range, samples, seconds, days) => `${range} · 원시 샘플 ${samples}개 · ${seconds}초 간격 · ${days}일 보관`],
        [/^剩餘 (.+)$/, (_, remaining) => `${remaining} 남음`]
      ]
    };
    for (const [pattern, replacer] of rules[targetLocale] || []) {
      if (pattern.test(source)) return source.replace(pattern, replacer);
    }
    return "";
  }

  function translate(source, targetLocale = locale) {
    const value = String(source ?? "");
    if (!value) return value;
    const alternate = alternateCatalogs[targetLocale]?.get(value);
    if (alternate) return alternate;
    if (targetLocale === "zh-TW") return value;
    const exact = catalogs[targetLocale]?.get(value);
    if (exact) return exact;
    const dynamic = translateDynamic(value, targetLocale);
    if (dynamic) return dynamic;
    if (targetLocale === "zh-CN") return simplify(value);
    return value;
  }

  function translatePreservingWhitespace(value) {
    const match = String(value).match(/^(\s*)([\s\S]*?)(\s*)$/);
    if (!match || !match[2]) return value;
    return `${match[1]}${translate(match[2])}${match[3]}`;
  }

  function ignored(node) {
    const element = node.nodeType === Node.ELEMENT_NODE ? node : node.parentElement;
    return !element || Boolean(element.closest("script, style, noscript, textarea, pre, code, [data-i18n-ignore], [contenteditable='true'], .provider-name, .key-name, .name, .masked"));
  }

  function translateTextNode(node) {
    if (!node?.nodeValue || ignored(node)) return;
    const current = node.nodeValue;
    const lastRendered = renderedTexts.get(node);
    if (!textSources.has(node) || current !== lastRendered) textSources.set(node, current);
    const source = textSources.get(node);
    const translated = translatePreservingWhitespace(source);
    renderedTexts.set(node, translated);
    if (current !== translated) node.nodeValue = translated;
  }

  function attributeState(element) {
    if (!attributeSources.has(element)) attributeSources.set(element, new Map());
    return attributeSources.get(element);
  }

  function translateAttribute(element, name) {
    if (!element.hasAttribute(name) || element.closest("script, style, noscript, pre, code, [data-i18n-ignore], [contenteditable='true'], .provider-name, .key-name, .name, .masked")) return;
    const state = attributeState(element);
    const current = element.getAttribute(name) || "";
    const existing = state.get(name);
    if (!existing || current !== existing.rendered) state.set(name, { source: current, rendered: current });
    const entry = state.get(name);
    const translated = translate(entry.source);
    entry.rendered = translated;
    if (current !== translated) element.setAttribute(name, translated);
  }

  function applyNode(root) {
    if (!root || ignored(root)) return;
    if (root.nodeType === Node.TEXT_NODE) {
      translateTextNode(root);
      return;
    }
    if (root.nodeType !== Node.ELEMENT_NODE && root.nodeType !== Node.DOCUMENT_NODE && root.nodeType !== Node.DOCUMENT_FRAGMENT_NODE) return;
    if (root.nodeType === Node.ELEMENT_NODE) {
      ["title", "aria-label", "placeholder", "alt", "label"].forEach((name) => translateAttribute(root, name));
    }
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
    let node = walker.nextNode();
    while (node) {
      if (node.nodeType === Node.TEXT_NODE) translateTextNode(node);
      else ["title", "aria-label", "placeholder", "alt", "label"].forEach((name) => translateAttribute(node, name));
      node = walker.nextNode();
    }
  }

  function applyDocument() {
    document.documentElement.lang = locale;
    applyNode(document.documentElement);
  }

  function notifyChange() {
    const detail = { preference, locale };
    window.dispatchEvent(new CustomEvent("lbp-language-changed", { detail }));
  }

  function usePreference(value, options = {}) {
    const nextPreference = normalizePreference(value);
    const nextLocale = resolveLocale(nextPreference);
    const changed = nextPreference !== preference || nextLocale !== locale;
    preference = nextPreference;
    locale = nextLocale;
    if (options.persist !== false) localStorage.setItem(STORAGE_KEY, preference);
    applyDocument();
    if (changed) notifyChange();
    if (options.broadcast !== false) channel?.postMessage({ preference });
    return { preference, locale };
  }

  function start() {
    applyDocument();
    observer = new MutationObserver((mutations) => {
      mutations.forEach((mutation) => {
        if (mutation.type === "characterData") translateTextNode(mutation.target);
        else if (mutation.type === "attributes") translateAttribute(mutation.target, mutation.attributeName);
        else mutation.addedNodes.forEach(applyNode);
      });
    });
    observer.observe(document.documentElement, {
      subtree: true,
      childList: true,
      characterData: true,
      attributes: true,
      attributeFilter: ["title", "aria-label", "placeholder", "alt", "label"]
    });
    notifyChange();
  }

  try {
    channel = new BroadcastChannel(CHANNEL_NAME);
    channel.addEventListener("message", (event) => usePreference(event.data?.preference, { persist: false, broadcast: false }));
  } catch (_error) {
    channel = null;
  }
  window.addEventListener("storage", (event) => {
    if (event.key === STORAGE_KEY) usePreference(event.newValue, { persist: false, broadcast: false });
  });

  window.LBPI18n = Object.freeze({
    storageKey: STORAGE_KEY,
    supported: Object.freeze(["auto", "zh-TW", "zh-CN", "en", "ja", "ko"]),
    preference: () => preference,
    locale: () => locale,
    setLanguage: (value) => usePreference(value),
    t: (source) => translate(source),
    apply: (root = document.documentElement) => applyNode(root),
    formatNumber: (value, options) => new Intl.NumberFormat(locale, options).format(value),
    formatDate: (value, options) => new Intl.DateTimeFormat(locale, options).format(value instanceof Date ? value : new Date(value))
  });

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start, { once: true });
  else start();
})();
