package main

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const tlsFullChainMaxCertificates = 6

func ensureTLSCertificateFullChain(certPath string) error {
	certPath = strings.TrimSpace(certPath)
	if certPath == "" {
		return nil
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	certs, err := parseCertificatesFromBytes(data)
	if err != nil {
		return err
	}
	if len(certs) == 0 {
		return fmt.Errorf("找不到 CERTIFICATE PEM 區塊")
	}

	updated := append([]*x509.Certificate{}, certs...)
	changed := false
	// TLS server certificate_list 不應包含 trust anchor。部分 TLS client 會把伺服器
	// 傳來但本機未信任的自簽 Root 視為 self-signed certificate in chain。
	for len(updated) > 1 && isSelfSignedCertificate(updated[len(updated)-1]) {
		updated = updated[:len(updated)-1]
		changed = true
	}
	if len(updated) > 1 && !certificateChainHasUsableIssuer(updated) {
		return fmt.Errorf("TLS 憑證鏈順序或簽章不正確；目前憑證數：%d", len(updated))
	}

	seen := map[string]bool{}
	for _, cert := range updated {
		seen[string(cert.RawSubject)] = true
	}

	for len(updated) < tlsFullChainMaxCertificates {
		last := updated[len(updated)-1]
		if isSelfSignedCertificate(last) {
			break
		}
		issuer, err := fetchIssuerCertificate(last)
		if err != nil {
			// 已具備 leaf + intermediate 且鏈可驗證時，無須因 Root AIA 無法下載
			// 而破壞既有可用鏈。只有 leaf 單張憑證才必須取得 issuer。
			if len(updated) > 1 && certificateChainHasUsableIssuer(updated) {
				break
			}
			return fmt.Errorf("下載 issuer certificate 失敗：%w", err)
		}
		key := string(issuer.RawSubject)
		if seen[key] {
			break
		}
		if isSelfSignedCertificate(issuer) {
			// Root CA 是 client 的 trust anchor，不附加到伺服器 fullchain。
			break
		}
		updated = append(updated, issuer)
		seen[key] = true
		changed = true
	}

	if len(updated) > 1 && !certificateChainHasUsableIssuer(updated) {
		return fmt.Errorf("無法自動補齊 fullchain；目前憑證數：%d", len(certs))
	}
	if len(updated) >= tlsFullChainMaxCertificates {
		return fmt.Errorf("無法自動補齊 fullchain；issuer 追蹤已達上限 %d 張憑證", tlsFullChainMaxCertificates)
	}
	if !changed {
		return nil
	}
	if err := backupTLSCertificateFile(certPath); err != nil {
		return err
	}
	if err := os.WriteFile(certPath, encodeCertificatesToPEM(updated), filePermOrDefault(certPath, 0644)); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "已正規化 TLS fullchain：%s（%d -> %d 張憑證，不包含 Root CA）\n", certPath, len(certs), len(updated))
	return nil
}

func parseCertificatesFromBytes(data []byte) ([]*x509.Certificate, error) {
	certs := []*x509.Certificate{}
	rest := data
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		if strings.EqualFold(strings.TrimSpace(block.Type), "CERTIFICATE") {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, err
			}
			certs = append(certs, cert)
		}
		rest = next
	}
	return certs, nil
}

func certificateChainHasUsableIssuer(certs []*x509.Certificate) bool {
	if len(certs) < 2 {
		return false
	}
	for i := 0; i+1 < len(certs); i++ {
		if err := certs[i].CheckSignatureFrom(certs[i+1]); err != nil {
			return false
		}
	}
	return true
}

func isSelfSignedCertificate(cert *x509.Certificate) bool {
	if cert == nil {
		return false
	}
	return cert.CheckSignatureFrom(cert) == nil
}

func fetchIssuerCertificate(cert *x509.Certificate) (*x509.Certificate, error) {
	if cert == nil || len(cert.IssuingCertificateURL) == 0 {
		return nil, fmt.Errorf("certificate 未提供 CA Issuers URL")
	}
	var lastErr error
	for _, rawURL := range cert.IssuingCertificateURL {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			continue
		}
		issuers, err := downloadCertificates(rawURL)
		if err == nil {
			for _, issuer := range issuers {
				if cert.CheckSignatureFrom(issuer) == nil {
					return issuer, nil
				}
			}
			err = fmt.Errorf("%s 未包含可簽發目前憑證的 issuer", rawURL)
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("certificate 未提供可用的 CA Issuers URL")
}

func downloadCertificates(rawURL string) ([]*x509.Certificate, error) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s 回應 HTTP %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if certs, err := parseCertificatesFromBytes(data); err == nil && len(certs) > 0 {
		return certs, nil
	}
	cert, err := x509.ParseCertificate(data)
	if err == nil {
		return []*x509.Certificate{cert}, nil
	}
	if certs, pkcs7Err := parseCertificatesWithOpenSSLPKCS7(data); pkcs7Err == nil && len(certs) > 0 {
		return certs, nil
	}
	return nil, err
}

func parseCertificatesWithOpenSSLPKCS7(data []byte) ([]*x509.Certificate, error) {
	opensslPath, err := exec.LookPath("openssl")
	if err != nil {
		return nil, err
	}
	tmpDir, err := os.MkdirTemp("", "llmproxy-pkcs7-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	inputPath := filepath.Join(tmpDir, "issuer.p7c")
	if err := os.WriteFile(inputPath, data, 0600); err != nil {
		return nil, err
	}
	for _, format := range []string{"DER", "PEM"} {
		out, err := exec.Command(opensslPath, "pkcs7", "-inform", format, "-print_certs", "-in", inputPath).Output()
		if err != nil {
			continue
		}
		if certs, parseErr := parseCertificatesFromBytes(out); parseErr == nil && len(certs) > 0 {
			return certs, nil
		}
	}
	return nil, fmt.Errorf("openssl 無法解析 PKCS#7 certificate bundle")
}

func encodeCertificatesToPEM(certs []*x509.Certificate) []byte {
	var out strings.Builder
	for _, cert := range certs {
		_ = pem.Encode(&out, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	}
	return []byte(out.String())
}

func backupTLSCertificateFile(certPath string) error {
	info, err := os.Stat(certPath)
	if err != nil {
		return err
	}
	backupPath := fmt.Sprintf("%s.pre-fullchain.%s.bak", certPath, time.Now().Format("20060102150405"))
	return copyFile(certPath, backupPath, info.Mode().Perm())
}

func copyFile(source string, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
