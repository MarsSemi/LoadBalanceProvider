package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -------------------------------------------------------------------------------------
func TestEncryptConfigSecretUsesV2AndDecryptsWithAgentProperties(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "agent.properties")
	_body := `{
  "default_pwd": "",
  "keychain": "test-config-secret"
}
`
	if _err := os.WriteFile(_path, []byte(_body), 0600); _err != nil {
		t.Fatalf("WriteFile error = %v", _err)
	}
	setConfigSecretPath(_path)

	_encrypted, _err := EncryptConfigSecret("secret-value")
	if _err != nil {
		t.Fatalf("EncryptConfigSecret error = %v", _err)
	}
	if !strings.HasPrefix(_encrypted, encryptedValuePrefix) {
		t.Fatalf("encrypted value prefix = %q, want %q", _encrypted, encryptedValuePrefix)
	}

	_decrypted, _err := DecryptConfigSecret(_encrypted)
	if _err != nil {
		t.Fatalf("DecryptConfigSecret error = %v", _err)
	}
	if _decrypted != "secret-value" {
		t.Fatalf("decrypted value = %q, want secret-value", _decrypted)
	}
}

// -------------------------------------------------------------------------------------
func TestEnsureAgentPropertiesSecretsEncryptedMigratesLegacyV1(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "agent.properties")
	_legacy, _err := legacyEncryptedForTest("legacy-password", "test-config-secret")
	if _err != nil {
		t.Fatalf("legacyEncryptedForTest error = %v", _err)
	}
	_body := `{
  "default_account": "test-account",
  "default_pwd": "` + _legacy + `",
  "keychain": "test-config-secret"
}
`
	if _err := os.WriteFile(_path, []byte(_body), 0600); _err != nil {
		t.Fatalf("WriteFile error = %v", _err)
	}

	if _err := EnsureAgentPropertiesSecretsEncrypted(_path); _err != nil {
		t.Fatalf("EnsureAgentPropertiesSecretsEncrypted error = %v", _err)
	}

	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		t.Fatalf("ReadFile error = %v", _err)
	}
	_text := string(_bytes)
	if strings.Contains(_text, legacyEncryptedValuePrefix) {
		t.Fatalf("agent properties still contains legacy encrypted value: %s", _text)
	}
	if !strings.Contains(_text, encryptedValuePrefix) {
		t.Fatalf("agent properties does not contain v2 encrypted value: %s", _text)
	}
	_config, _err := LoadAgentConfig(_path)
	if _err != nil {
		t.Fatalf("LoadAgentConfig error = %v", _err)
	}
	if _config.DefaultPassword != "legacy-password" {
		t.Fatalf("default password = %q, want legacy-password", _config.DefaultPassword)
	}
}

// -------------------------------------------------------------------------------------
func TestEnsureAgentPropertiesSecretsEncryptedGeneratesKeychain(t *testing.T) {
	_path := filepath.Join(t.TempDir(), "agent.properties")
	_body := `{
  "default_account": "admin",
  "default_pwd": "change-me",
  "keychain": ""
}
`
	if _err := os.WriteFile(_path, []byte(_body), 0644); _err != nil {
		t.Fatalf("WriteFile error = %v", _err)
	}

	_err := EnsureAgentPropertiesSecretsEncrypted(_path)
	if _err != nil {
		t.Fatalf("EnsureAgentPropertiesSecretsEncrypted error = %v", _err)
	}

	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		t.Fatalf("ReadFile error = %v", _err)
	}
	var _raw map[string]interface{}
	if _err := json.Unmarshal(_bytes, &_raw); _err != nil {
		t.Fatalf("Unmarshal error = %v", _err)
	}
	_keychain, _ok := _raw[configSecretField].(string)
	_identifier := strings.TrimSuffix(_keychain, legacyConfigSecretSuffix)
	if !_ok || !strings.HasSuffix(_keychain, legacyConfigSecretSuffix) || len(_identifier) != configSecretIDLength {
		t.Fatalf("generated keychain = %q", _keychain)
	}
	for _, _character := range _identifier {
		if !strings.ContainsRune(configSecretIDAlphabet, _character) {
			t.Fatalf("generated keychain contains invalid character: %q", _keychain)
		}
	}
	_password, _ok := _raw["default_pwd"].(string)
	if !_ok || !strings.HasPrefix(_password, encryptedValuePrefix) {
		t.Fatalf("default_pwd was not encrypted with generated keychain")
	}
	_info, _err := os.Stat(_path)
	if _err != nil {
		t.Fatalf("Stat error = %v", _err)
	}
	if _info.Mode().Perm() != 0600 {
		t.Fatalf("agent.properties mode = %o, want 600", _info.Mode().Perm())
	}
}

// -------------------------------------------------------------------------------------
func legacyEncryptedForTest(_plainText string, _secret string) (string, error) {
	_block, _err := aes.NewCipher(deriveConfigSecretKey(_secret))
	if _err != nil {
		return "", _err
	}
	_gcm, _err := cipher.NewGCM(_block)
	if _err != nil {
		return "", _err
	}
	_nonce := make([]byte, _gcm.NonceSize())
	if _, _err := rand.Read(_nonce); _err != nil {
		return "", _err
	}
	_cipherText := _gcm.Seal(nil, _nonce, []byte(_plainText), nil)
	_payload := append(_nonce, _cipherText...)
	return legacyEncryptedValuePrefix + base64.RawURLEncoding.EncodeToString(_payload), nil
}
