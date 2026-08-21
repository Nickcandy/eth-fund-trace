package ethaddr

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalid = errors.New("invalid ethereum address")
	pattern    = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
)

func Normalize(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if !pattern.MatchString(value) {
		return "", ErrInvalid
	}
	return value, nil
}
