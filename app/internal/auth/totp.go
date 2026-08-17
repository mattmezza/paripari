package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"strings"

	"github.com/mattmezza/paripari/internal/model"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	issuerName = "PariPari"
	// RecoveryCodeCount is how many single-use backup codes a user gets.
	RecoveryCodeCount = 8
	// recoveryAlphabet avoids look-alikes (0/O, 1/I/L).
	recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	// recoveryCodeLen in characters; 20 chars from a 32-symbol alphabet ≈ 100
	// bits of entropy per code.
	recoveryCodeLen = 20
)

var b32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret produces a new enrollment secret for the user and the
// otpauth:// URI for their authenticator app.
func GenerateTOTPSecret(email string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuerName,
		AccountName: email,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// TOTPURI rebuilds the otpauth:// URI from a stored base32 secret, for the QR
// code shown while enrollment is pending.
func TOTPURI(email, secret string) (string, error) {
	raw, err := b32NoPadding.DecodeString(secret)
	if err != nil {
		return "", err
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuerName,
		AccountName: email,
		Secret:      raw,
	})
	if err != nil {
		return "", err
	}
	return key.URL(), nil
}

// TOTPQRDataURI renders the otpauth URI as a PNG QR code data URI for the
// settings page.
func TOTPQRDataURI(uri string) (string, error) {
	key, err := otp.NewKeyFromURL(uri)
	if err != nil {
		return "", err
	}
	img, err := key.Image(220, 220)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// ValidateTOTP checks a 6-digit code against the user's secret. An empty
// secret never validates.
func ValidateTOTP(secret, code string) bool {
	if secret == "" || strings.TrimSpace(code) == "" {
		return false
	}
	return totp.Validate(strings.TrimSpace(code), secret)
}

// --- recovery codes ---

// GenerateRecoveryCodes returns RecoveryCodeCount fresh single-use codes in
// "XXXX-XXXX-XXXX-XXXX-XXXX" form.
func GenerateRecoveryCodes() ([]string, error) {
	out := make([]string, RecoveryCodeCount)
	for i := range out {
		b := make([]byte, recoveryCodeLen)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		var sb strings.Builder
		for j, by := range b {
			if j > 0 && j%4 == 0 {
				sb.WriteByte('-')
			}
			sb.WriteByte(recoveryAlphabet[int(by)%len(recoveryAlphabet)])
		}
		out[i] = sb.String()
	}
	return out, nil
}

// normalizeRecoveryCode strips separators and case so "abcd-efgh" and
// "ABCDEFGH" compare equal.
func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(code)
	code = strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, code)
	return code
}

// HashRecoveryCode returns the stored form: SHA-256 hex of the normalized code.
func HashRecoveryCode(code string) string {
	h := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(h[:])
}

// VerifyRecoveryCode checks a code against the user's stored digests and, on a
// match, returns the updated remaining-code list (the used one removed).
func VerifyRecoveryCode(u *model.User, code string) (remaining []string, ok bool) {
	want := HashRecoveryCode(code)
	for i, have := range u.RecoveryCodes {
		if subtle.ConstantTimeCompare([]byte(have), []byte(want)) == 1 {
			rest := make([]string, 0, len(u.RecoveryCodes)-1)
			rest = append(rest, u.RecoveryCodes[:i]...)
			rest = append(rest, u.RecoveryCodes[i+1:]...)
			return rest, true
		}
	}
	return u.RecoveryCodes, false
}

// ParseRecoveryCodes decodes the stored JSON list; a missing/empty value reads
// as no codes.
func ParseRecoveryCodes(raw string) []string {
	var codes []string
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &codes); err != nil {
		return nil
	}
	return codes
}
