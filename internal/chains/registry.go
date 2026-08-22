package chains

import (
	"errors"
	"strings"
)

var ErrUnsupported = errors.New("unsupported chain")

type Chain struct {
	Name string `json:"name"`
	ID   int64  `json:"chainId"`
}

var supported = map[string]Chain{
	"ethereum": {Name: "ethereum", ID: 1},
	"base":     {Name: "base", ID: 8453},
}

func Resolve(value string) (Chain, error) {
	name := strings.ToLower(strings.TrimSpace(value))
	if name == "" {
		name = "ethereum"
	}
	chain, ok := supported[name]
	if !ok {
		return Chain{}, ErrUnsupported
	}
	return chain, nil
}

func Supported() []Chain { return []Chain{supported["ethereum"], supported["base"]} }
