package model

import (
	"crypto/rand"
	"math/big"
)

// RandomIntn returns a random int in [0, n).
func RandomIntn(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(v.Int64())
}
