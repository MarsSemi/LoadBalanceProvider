package service

import (
	"fmt"
	"time"

	"LoadBalanceProvider/src/balancer"

	"github.com/MarsSemi/MarsCloud-SaaS/SDK/MarsJSON"
	"github.com/MarsSemi/MarsCloud-SaaS/SDK/MarsService"
	"github.com/MarsSemi/MarsCloud-SaaS/SDK/Tools"
)

// -------------------------------------------------------------------------------------
// CloudService 繼承自 MarsService，負責服務生命週期與背景監控。
// -------------------------------------------------------------------------------------
type CloudService struct {
	*MarsService.MarsService
	Balancer *balancer.LoadBalancer
}

// -------------------------------------------------------------------------------------
func (_s *CloudService) Process() {
	Tools.Log.Print(Tools.LL_Info, "LoadBalanceProvider 主程序啟動...")

	for {
		if _s.Balancer != nil {
			Tools.Log.Print(Tools.LL_Info, fmt.Sprintf("LLM Provider 狀態: %s", _s.Balancer.StatusText()))
		}

		time.Sleep(30 * time.Second)
	}
}

// -------------------------------------------------------------------------------------
func (_s *CloudService) OnMQTTConnected() {
	Tools.Log.Print(Tools.LL_Debug, "OnMQTTConnected")
}

// -------------------------------------------------------------------------------------
func (_s *CloudService) OnMQTTConnectionLost(_err error) {
	Tools.Log.Print(Tools.LL_Debug, "OnMQTTConnectionLost")
}

// -------------------------------------------------------------------------------------
func (_s *CloudService) OnMQTTMessage(_topic string, _payload string) {
	Tools.Log.Print(Tools.LL_Debug, fmt.Sprintf("Get MQTT : %s, 內容: %s", _topic, _payload))
}

// -------------------------------------------------------------------------------------
func (_s *CloudService) OnPropertyChange(_property *MarsJSON.JSONObject) {
	Tools.Log.Print(Tools.LL_Debug, "OnPropertyChange")
}

// -------------------------------------------------------------------------------------
func (_s *CloudService) BeforeServiceStop() {
	Tools.Log.Print(Tools.LL_Debug, "My BeforeServiceStop Callback")
}

// -------------------------------------------------------------------------------------
