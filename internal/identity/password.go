package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	passwordpolicy "github.com/kakj-go/Argus/internal/gen/openapi/passwordpolicy"
)

const (
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLength  = 16
	argonHashLength  = 32
)

var weakPasswords = func() map[string]struct{} {
	result := make(map[string]struct{}, len(passwordpolicy.CommonPasswords))
	for _, password := range passwordpolicy.CommonPasswords {
		result[password] = struct{}{}
	}
	return result
}()

var ErrWeakPassword = errors.New("password does not satisfy the Argus password policy")

type PasswordRule string

const (
	PasswordRuleMinLength      PasswordRule = passwordpolicy.RuleMinLength
	PasswordRuleMaxLength      PasswordRule = passwordpolicy.RuleMaxLength
	PasswordRuleLetterRequired PasswordRule = passwordpolicy.RuleLetterRequired
	PasswordRuleDigitRequired  PasswordRule = passwordpolicy.RuleDigitRequired
	PasswordRuleCommon         PasswordRule = passwordpolicy.RuleCommonPassword
	PasswordRuleIdentity       PasswordRule = passwordpolicy.RuleContainsIdentity
	PasswordRuleReused         PasswordRule = passwordpolicy.RuleReusedPassword
)

const (
	PasswordMinLength = passwordpolicy.MinLength
	PasswordMaxLength = passwordpolicy.MaxLength
)

type PasswordPolicyError struct {
	Rule PasswordRule
}

func (err PasswordPolicyError) Error() string { return ErrWeakPassword.Error() }
func (err PasswordPolicyError) Unwrap() error { return ErrWeakPassword }

func PasswordRuleFromError(err error) (PasswordRule, bool) {
	var policyError PasswordPolicyError
	if !errors.As(err, &policyError) {
		return "", false
	}
	return policyError.Rule, true
}

func ValidatePassword(password, username, email string) error {
	length := utf8.RuneCountInString(password)
	if length < PasswordMinLength {
		return PasswordPolicyError{Rule: PasswordRuleMinLength}
	}
	if length > PasswordMaxLength {
		return PasswordPolicyError{Rule: PasswordRuleMaxLength}
	}
	hasLetter := false
	hasDigit := false
	for _, character := range password {
		hasLetter = hasLetter || unicode.IsLetter(character)
		hasDigit = hasDigit || unicode.IsDigit(character)
	}
	if !hasLetter {
		return PasswordPolicyError{Rule: PasswordRuleLetterRequired}
	}
	if !hasDigit {
		return PasswordPolicyError{Rule: PasswordRuleDigitRequired}
	}
	normalized := strings.ToLower(password)
	if _, weak := weakPasswords[normalized]; weak {
		return PasswordPolicyError{Rule: PasswordRuleCommon}
	}
	for _, source := range []string{username, strings.SplitN(email, "@", 2)[0]} {
		fragment := strings.ToLower(strings.TrimSpace(source))
		if utf8.RuneCountInString(fragment) >= 3 && strings.Contains(normalized, fragment) {
			return PasswordPolicyError{Rule: PasswordRuleIdentity}
		}
	}
	return nil
}

func ValidatePasswordChange(password, previousPassword, username, email string) error {
	if err := ValidatePassword(password, username, email); err != nil {
		return err
	}
	if len(password) == len(previousPassword) && subtle.ConstantTimeCompare([]byte(password), []byte(previousPassword)) == 1 {
		return PasswordPolicyError{Rule: PasswordRuleReused}
	}
	return nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonHashLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("invalid Argon2id encoding")
	}
	var memory uint64
	var iterations uint64
	var parallelism uint64
	for _, item := range strings.Split(parts[3], ",") {
		keyValue := strings.SplitN(item, "=", 2)
		if len(keyValue) != 2 {
			return false, errors.New("invalid Argon2id parameters")
		}
		value, err := strconv.ParseUint(keyValue[1], 10, 32)
		if err != nil {
			return false, errors.New("invalid Argon2id parameter value")
		}
		switch keyValue[0] {
		case "m":
			memory = value
		case "t":
			iterations = value
		case "p":
			parallelism = value
		}
	}
	if memory == 0 || iterations == 0 || parallelism == 0 || parallelism > 255 {
		return false, errors.New("incomplete Argon2id parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("invalid Argon2id salt")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false, errors.New("invalid Argon2id hash")
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
