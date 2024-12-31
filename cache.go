package cache

import "github.com/go-rat/cache/contracts"

func NewCache() contracts.Driver {
	return &Memory{}
}
