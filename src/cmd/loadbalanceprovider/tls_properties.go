package main

import (
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ensureTLSPemProperties mirrors AgenticService2 的憑證啟動檢查：
// - 偵測 ssl_key / ssl_key_file 是否放反並自動交換
// - 確認 PEM 類型
// - 憑證缺少中繼鏈時，依 AIA 自動補成 fullchain
func ensureTLSPemProperties(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "檢查 TLS 設定失敗：讀取 %s 失敗：%v\n", path, err)
		return
	}

	text := string(data)
	certValue, certOK := readPropertiesStringValue(text, "ssl_key")
	keyValue, keyOK := readPropertiesStringValue(text, "ssl_key_file")
	if !certOK || !keyOK {
		return
	}

	certValue = strings.TrimSpace(certValue)
	keyValue = strings.TrimSpace(keyValue)
	if certValue == "" || keyValue == "" {
		return
	}

	ext := strings.ToLower(filepath.Ext(certValue))
	if ext == ".p12" || ext == ".pfx" {
		return
	}

	baseDir := filepath.Dir(path)
	certPath := resolvePropertyPath(baseDir, certValue)
	keyPath := resolvePropertyPath(baseDir, keyValue)
	certKind, certErr := inspectPEMFile(certPath)
	keyKind, keyErr := inspectPEMFile(keyPath)
	if certErr != nil || keyErr != nil {
		if certErr != nil {
			fmt.Fprintf(os.Stderr, "檢查 ssl_key 失敗：%s：%v\n", certValue, certErr)
		}
		if keyErr != nil {
			fmt.Fprintf(os.Stderr, "檢查 ssl_key_file 失敗：%s：%v\n", keyValue, keyErr)
		}
		return
	}

	if certKind.hasPrivateKey && !certKind.hasCertificate && keyKind.hasCertificate && !keyKind.hasPrivateKey {
		changedText, changed := setPropertiesStringValues(text, map[string]string{
			"ssl_key":      keyValue,
			"ssl_key_file": certValue,
		})
		if changed {
			if err := os.WriteFile(path, []byte(changedText), filePermOrDefault(path, 0600)); err != nil {
				fmt.Fprintf(os.Stderr, "修正 TLS 憑證設定失敗：%v\n", err)
				return
			}
			fmt.Fprintf(os.Stdout, "已修正 TLS 憑證設定：ssl_key 與 ssl_key_file 原本疑似放反，已自動交換。\n")
		}
		return
	}

	if certKind.hasPrivateKey && !certKind.hasCertificate {
		fmt.Fprintf(os.Stderr, "TLS 設定可能錯誤：ssl_key 應該是憑證檔 (.crt/.pem)，但目前看起來是私鑰檔：%s\n", certValue)
	}
	if keyKind.hasCertificate && !keyKind.hasPrivateKey {
		fmt.Fprintf(os.Stderr, "TLS 設定可能錯誤：ssl_key_file 應該是私鑰檔 (.key)，但目前看起來是憑證檔：%s\n", keyValue)
	}
	if certKind.hasCertificate && !certKind.hasPrivateKey {
		if err := ensureTLSCertificateFullChain(certPath); err != nil {
			fmt.Fprintf(os.Stderr, "檢查 TLS fullchain 失敗：%s：%v\n", certValue, err)
		}
	}
}

type pemFileKind struct {
	hasCertificate bool
	hasPrivateKey  bool
}

func inspectPEMFile(path string) (pemFileKind, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pemFileKind{}, err
	}

	var result pemFileKind
	rest := data
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		blockType := strings.ToUpper(strings.TrimSpace(block.Type))
		if strings.Contains(blockType, "CERTIFICATE") {
			result.hasCertificate = true
		}
		if strings.Contains(blockType, "PRIVATE KEY") {
			result.hasPrivateKey = true
		}
		rest = next
	}
	if !result.hasCertificate && !result.hasPrivateKey {
		return result, fmt.Errorf("找不到可辨識的 CERTIFICATE 或 PRIVATE KEY PEM 區塊")
	}
	return result, nil
}

func resolvePropertyPath(baseDir string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, strings.TrimPrefix(value, "~/"))
		}
	}
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}

func readPropertiesStringValue(text string, key string) (string, bool) {
	quotedKey := regexp.QuoteMeta(key)
	pattern := regexp.MustCompile(`(?m)^\s*"` + quotedKey + `"\s*:\s*"([^"]*)"`)
	match := pattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func setPropertiesStringValues(text string, values map[string]string) (string, bool) {
	changed := false
	for key, value := range values {
		quotedKey := regexp.QuoteMeta(key)
		pattern := regexp.MustCompile(`(?m)^(\s*"` + quotedKey + `"\s*:\s*")([^"]*)(")`)
		text = pattern.ReplaceAllStringFunc(text, func(line string) string {
			match := pattern.FindStringSubmatch(line)
			if len(match) != 4 || match[2] == value {
				return line
			}
			changed = true
			return match[1] + value + match[3]
		})
	}
	return text, changed
}

func filePermOrDefault(path string, fallback os.FileMode) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return fallback
}
