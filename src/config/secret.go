package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// -------------------------------------------------------------------------------------
const (
	encryptedValuePrefix       = "enc:v2:"
	legacyEncryptedValuePrefix = "enc:v1:"
	configSecretField          = "keychain"
	legacyConfigSecretSuffix   = ":agent-config-secret:v1"
	configSecretIDLength       = 16
	configSecretIDAlphabet     = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

var (
	configSecretPathMu sync.RWMutex
	configSecretPath   = "agent.properties"
)

// -------------------------------------------------------------------------------------
func EncryptConfigSecret(_plainText string) (string, error) {
	if strings.TrimSpace(_plainText) == "" || IsEncryptedConfigSecret(_plainText) {
		return _plainText, nil
	}

	_secret, _err := configSecret()
	if _err != nil {
		return "", _err
	}
	return encryptConfigSecretWithSecret(_plainText, _secret)
}

// -------------------------------------------------------------------------------------
func DecryptConfigSecret(_value string) (string, error) {
	if !IsEncryptedConfigSecret(_value) {
		return _value, nil
	}

	_secret, _err := configSecret()
	if _err != nil {
		return "", _err
	}
	return decryptConfigSecretWithSecret(_value, _secret)
}

// -------------------------------------------------------------------------------------
func IsEncryptedConfigSecret(_value string) bool {
	_value = strings.TrimSpace(_value)
	return strings.HasPrefix(_value, encryptedValuePrefix) || strings.HasPrefix(_value, legacyEncryptedValuePrefix)
}

// -------------------------------------------------------------------------------------
func EnsureAgentPropertiesSecretsEncrypted(_agentPath string) error {
	setConfigSecretPath(_agentPath)

	_bytes, _err := os.ReadFile(_agentPath)
	if _err != nil {
		return _err
	}

	var _raw map[string]interface{}
	if _err := json.Unmarshal(_bytes, &_raw); _err != nil {
		return _err
	}

	_changed := false
	_secretValue, _exists := _raw[configSecretField]
	_secret, _valid := _secretValue.(string)
	if _exists && !_valid {
		return fmt.Errorf("%s must be a string", configSecretField)
	}
	_secret = strings.TrimSpace(_secret)
	if _secret == "" {
		_secret, _err = generateConfigSecret()
		if _err != nil {
			return _err
		}
		_raw[configSecretField] = _secret
		_changed = true
	}

	for _, _field := range []string{"default_pwd"} {
		_value, _ok := _raw[_field].(string)
		if !_ok || strings.TrimSpace(_value) == "" {
			continue
		}

		if IsEncryptedConfigSecret(_value) {
			_decrypted, _decryptErr := decryptConfigSecretWithSecret(_value, _secret)
			if _decryptErr != nil {
				return fmt.Errorf("%s decrypt failed with agent properties %s: %w", _field, configSecretField, _decryptErr)
			}
			if strings.HasPrefix(strings.TrimSpace(_value), encryptedValuePrefix) {
				continue
			}
			_value = _decrypted
		}

		_encrypted, _encryptErr := encryptConfigSecretWithSecret(_value, _secret)
		if _encryptErr != nil {
			return fmt.Errorf("%s encrypt failed: %w", _field, _encryptErr)
		}
		_raw[_field] = _encrypted
		_changed = true
	}

	if !_changed {
		return os.Chmod(_agentPath, 0600)
	}

	_bytes, _err = json.MarshalIndent(_raw, "", "  ")
	if _err != nil {
		return _err
	}
	if _err := os.WriteFile(_agentPath, append(_bytes, '\n'), 0600); _err != nil {
		return _err
	}
	return os.Chmod(_agentPath, 0600)
}

// -------------------------------------------------------------------------------------
func configSecret() (string, error) {
	configSecretPathMu.RLock()
	_path := configSecretPath
	configSecretPathMu.RUnlock()

	_bytes, _err := os.ReadFile(_path)
	if _err != nil {
		return "", fmt.Errorf("agent properties config secret read failed: %w", _err)
	}
	var _raw map[string]interface{}
	if _err := json.Unmarshal(_bytes, &_raw); _err != nil {
		return "", fmt.Errorf("agent properties config secret parse failed: %w", _err)
	}
	_secret, _ok := _raw[configSecretField].(string)
	if !_ok || strings.TrimSpace(_secret) == "" {
		return "", fmt.Errorf("agent properties %s is empty", configSecretField)
	}
	return strings.TrimSpace(_secret), nil
}

// -------------------------------------------------------------------------------------
func setConfigSecretPath(_path string) {
	if strings.TrimSpace(_path) == "" {
		return
	}
	configSecretPathMu.Lock()
	configSecretPath = _path
	configSecretPathMu.Unlock()
}

// -------------------------------------------------------------------------------------
func generateConfigSecret() (string, error) {
	_identifier := make([]byte, 0, configSecretIDLength)
	_randomBytes := make([]byte, configSecretIDLength)
	_acceptedByteLimit := byte(256 - (256 % len(configSecretIDAlphabet)))

	for len(_identifier) < configSecretIDLength {
		if _, _err := io.ReadFull(rand.Reader, _randomBytes); _err != nil {
			return "", _err
		}
		for _, _randomByte := range _randomBytes {
			if _randomByte >= _acceptedByteLimit {
				continue
			}
			_identifier = append(_identifier, configSecretIDAlphabet[int(_randomByte)%len(configSecretIDAlphabet)])
			if len(_identifier) == configSecretIDLength {
				break
			}
		}
	}
	return string(_identifier) + legacyConfigSecretSuffix, nil
}

// -------------------------------------------------------------------------------------
func deriveConfigSecretKey(_secret string) []byte {
	_hash := sha256.Sum256([]byte(_secret))
	return _hash[:]
}

// -------------------------------------------------------------------------------------
func encryptConfigSecretWithSecret(_plainText string, _secret string) (string, error) {
	if strings.TrimSpace(_plainText) == "" || IsEncryptedConfigSecret(_plainText) {
		return _plainText, nil
	}

	_block, _err := aes.NewCipher(deriveConfigSecretKey(_secret))
	if _err != nil {
		return "", _err
	}
	_gcm, _err := cipher.NewGCM(_block)
	if _err != nil {
		return "", _err
	}

	_nonce := make([]byte, _gcm.NonceSize())
	if _, _err := io.ReadFull(rand.Reader, _nonce); _err != nil {
		return "", _err
	}

	_cipherText := _gcm.Seal(nil, _nonce, []byte(_plainText), nil)
	_payload := append(_nonce, _cipherText...)
	return encryptedValuePrefix + base64.RawURLEncoding.EncodeToString(_payload), nil
}

// -------------------------------------------------------------------------------------
func decryptConfigSecretWithSecret(_value string, _secret string) (string, error) {
	if !IsEncryptedConfigSecret(_value) {
		return _value, nil
	}

	_payloadText := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(_value), encryptedValuePrefix), legacyEncryptedValuePrefix)
	_payload, _err := base64.RawURLEncoding.DecodeString(_payloadText)
	if _err != nil {
		return "", _err
	}
	return decryptConfigSecretPayload(_payload, deriveConfigSecretKey(_secret))
}

// -------------------------------------------------------------------------------------
func decryptConfigSecretPayload(_payload []byte, _key []byte) (string, error) {
	_block, _err := aes.NewCipher(_key)
	if _err != nil {
		return "", _err
	}
	_gcm, _err := cipher.NewGCM(_block)
	if _err != nil {
		return "", _err
	}
	if len(_payload) < _gcm.NonceSize() {
		return "", fmt.Errorf("encrypted config secret payload is too short")
	}

	_nonce := _payload[:_gcm.NonceSize()]
	_cipherText := _payload[_gcm.NonceSize():]
	_plainText, _err := _gcm.Open(nil, _nonce, _cipherText, nil)
	if _err != nil {
		return "", _err
	}
	return string(_plainText), nil
}
