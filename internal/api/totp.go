package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
	"github.com/pquerna/otp/totp"
)

// TOTP parameters follow RFC 6238 / the pquerna defaults every mainstream
// authenticator implements: SHA-1, 6 digits, 30-second period, ±1 step skew.
const (
	totpIssuer     = "Veil"
	totpSecretSize = 20 // 160-bit shared secret (Base32 -> 32 chars)
	totpPeriod     = 30
	totpSkew       = 1
)

// recoveryCodeCount is the number of single-use recovery codes shown once at
// TOTP confirmation.
const recoveryCodeCount = 10

// recoveryCodeBytes gives 5 random bytes per code -> 8 Base32 chars rendered
// as "XXXX-XXXX": 2^40 space per code, and code consumption is additionally
// bound by the login-family rate limits, so online guessing is not viable.
const recoveryCodeBytes = 5

var recoveryCodeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

var errTOTPSecretGeneration = errors.New("totp secret generation failed")

// generateTOTPSecret creates a fresh TOTP key for the account. The returned
// secret is the Base32 shared secret (stored encrypted, used for manual
// entry); uri is the otpauth:// provisioning URI rendered as a QR.
func generateTOTPSecret(username string) (secret string, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: username,
		Period:      totpPeriod,
		SecretSize:  totpSecretSize,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", errTOTPSecretGeneration
	}
	return key.Secret(), key.URL(), nil
}

// validateTOTPCode checks a 6-digit code against the secret at the given time
// with ±1 period of clock-skew tolerance. On success it returns the matched
// timestep so the caller can record it as the account's anti-replay watermark:
// RFC 6238 §5.2 requires the verifier to reject a second presentation of an
// already-accepted OTP within the same step (#1220).
func validateTOTPCode(now time.Time, secret, code string) (int64, bool) {
	code = strings.TrimSpace(code)
	secret = strings.TrimSpace(secret)
	if secret == "" || code == "" {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	// Same counter enumeration as totp.ValidateCustom: t, then t+i, t-i per
	// skew step.
	steps := []int64{current}
	for i := int64(1); i <= totpSkew; i++ {
		steps = append(steps, current+i, current-i)
	}
	for _, step := range steps {
		if step < 0 {
			continue
		}
		ok, err := hotp.ValidateCustom(code, uint64(step), secret, hotp.ValidateOpts{
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false
		}
		if ok {
			return step, true
		}
	}
	return 0, false
}

// generateRecoveryCodes returns count plaintext codes ("XXXX-XXXX") and their
// SHA-256 hashes. Only the hashes are persisted; the plaintext list is
// returned to the caller exactly once.
func generateRecoveryCodes() (codes []string, hashes []string, err error) {
	codes = make([]string, 0, recoveryCodeCount)
	hashes = make([]string, 0, recoveryCodeCount)
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		encoded := recoveryCodeEncoding.EncodeToString(raw)
		code := encoded[:4] + "-" + encoded[4:]
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

// normalizeRecoveryCode folds user input into the canonical lookup form:
// uppercase, no dashes or whitespace.
func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(code)
	code = strings.ReplaceAll(code, "-", "")
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, code)
}

// hashRecoveryCode is the at-rest form: SHA-256 of the normalized code. Codes
// are already high-entropy random strings, so a fast hash is the right
// primitive (bcrypt would add nothing but latency for an unauthenticated
// compare that must stay cheap — the code space is random, not chosen).
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

// consumeRecoveryCodeHash matches the supplied code against the stored hashes
// in constant time and, on success, returns the hash list with the spent code
// removed. Recovery codes are single-use; the caller persists the returned
// list BEFORE the login is allowed to complete (#1172).
func consumeRecoveryCodeHash(hashes []string, code string) (remaining []string, ok bool) {
	normalized := hashRecoveryCode(code)
	match := -1
	for i, stored := range hashes {
		if subtle.ConstantTimeCompare([]byte(stored), []byte(normalized)) == 1 {
			match = i
		}
	}
	if match < 0 {
		return hashes, false
	}
	remaining = append([]string(nil), hashes[:match]...)
	remaining = append(remaining, hashes[match+1:]...)
	return remaining, true
}
