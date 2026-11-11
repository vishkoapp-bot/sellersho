package warehouseopt

import (
	"errors"
	"math/big"
)

// Convert millimetres to cubic centimetres, rounding required capacity UP.
func Capacity(l, w, h, grams, quantity int64) (int64, int64, error) {
	if l <= 0 || w <= 0 || h <= 0 || grams <= 0 || quantity <= 0 {
		return 0, 0, errors.New("ابعاد، وزن و تعداد باید مثبت باشند")
	}
	v := big.NewInt(l)
	for _, n := range []int64{w, h, quantity} {
		v.Mul(v, big.NewInt(n))
	}
	v.Add(v, big.NewInt(999))
	v.Quo(v, big.NewInt(1000))
	weight := new(big.Int).Mul(big.NewInt(grams), big.NewInt(quantity))
	if !v.IsInt64() || !weight.IsInt64() {
		return 0, 0, errors.New("ظرفیت خارج از محدوده است")
	}
	return v.Int64(), weight.Int64(), nil
}
