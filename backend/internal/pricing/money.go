package pricing

// MoneyPolicyVersion applies only to new computations; paid order snapshots are
// immutable. All money here is nonnegative integer TOMAN, never rial. PSP unit
// conversion belongs to FIN-010's adapter, exactly once.
// Percent inputs have at most two decimal places (basis points), 0..1000%.
// Price stages round to nearest 100, half up, then floor at the input cost.
// Fees truncate toward zero. Exact refunds and allocations never round to 100.
import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

const MoneyPolicyVersion = "toman-bps-nearest100-floor-v1"

// JSON number consumers use JavaScript numbers, so new API money/aggregate
// totals are bounded by the largest exactly representable integer.
const MaxTransportMoney int64 = 9007199254740991

func MoneyFromNumber(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > float64(MaxTransportMoney) || math.Trunc(value) != value {
		return 0, ErrInvalidMoney
	}
	return int64(value), nil
}

func AccumulateMoney(total *int64, unit, quantity int64) error {
	part, err := CheckedMultiply(unit, quantity)
	if err != nil {
		return err
	}
	next, err := CheckedAdd(*total, part)
	if err != nil || next > MaxTransportMoney {
		return ErrInvalidMoney
	}
	*total = next
	return nil
}

func ValidateDiscount(mode string, value float64) error {
	if mode == "fixed" {
		_, err := MoneyFromNumber(value)
		return err
	}
	if mode != "percent" {
		return ErrInvalidMoney
	}
	bps, err := ParsePercent(value)
	if err != nil || bps > 10000 {
		return ErrInvalidMoney
	}
	return nil
}

func ValidateSettings(s *PlatformPricingSettings) error {
	if s == nil {
		return ErrInvalidMoney
	}
	for _, value := range []float64{s.PlatformPct, s.CentralProfitPct, s.IncreaseCapPct, s.MaxProfitMargin} {
		if _, err := ParsePercent(value); err != nil {
			return err
		}
	}
	if s.CentralProfitPct > 100 {
		return ErrInvalidMoney
	}
	return nil
}

var ErrInvalidMoney = errors.New("invalid money, percentage precision, or monetary overflow")

const percentDenominator int64 = 10000

var jsonDecimalToken = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// Bound work before constructing arbitrary precision numbers. These generous
// format limits still exceed the precision/range accepted by the money policy.
func boundedDecimal(value string) (*big.Rat, error) {
	if len(value) == 0 || len(value) > 128 || !jsonDecimalToken.MatchString(value) {
		return nil, ErrInvalidMoney
	}
	if i := strings.IndexAny(value, "eE"); i >= 0 {
		exponent, err := strconv.ParseInt(value[i+1:], 10, 16)
		if err != nil || exponent < -128 || exponent > 128 {
			return nil, ErrInvalidMoney
		}
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, ErrInvalidMoney
	}
	return r, nil
}

// ParsePercent translates the shortest decimal representation of the legacy
// float transport into an exact scaled integer. No float multiplication is
// used, and a value with unsupported precision is quarantined, never rounded.
func ParsePercent(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1000 {
		return 0, ErrInvalidMoney
	}
	return ParsePercentDecimal(strconv.FormatFloat(value, 'f', -1, 64))
}

// ParsePercentDecimal validates the original decimal token before compatibility
// float transport can erase unsupported digits.
func ParsePercentDecimal(value string) (int64, error) {
	r, err := boundedDecimal(value)
	if err != nil {
		return 0, ErrInvalidMoney
	}
	r.Mul(r, big.NewRat(100, 1))
	if !r.IsInt() || !r.Num().IsInt64() || r.Sign() < 0 || r.Num().Cmp(big.NewInt(100000)) > 0 {
		return 0, ErrInvalidMoney
	}
	return r.Num().Int64(), nil
}

func MoneyFromDecimal(value string) (int64, error) {
	r, err := boundedDecimal(value)
	if err != nil || !r.IsInt() || !r.Num().IsInt64() || r.Sign() < 0 || r.Num().Cmp(big.NewInt(MaxTransportMoney)) > 0 {
		return 0, ErrInvalidMoney
	}
	return r.Num().Int64(), nil
}

func CheckedAdd(a, b int64) (int64, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, ErrInvalidMoney
	}
	return a + b, nil
}

func CheckedMultiply(a, b int64) (int64, error) {
	if a < 0 || b < 0 || (b != 0 && a > math.MaxInt64/b) {
		return 0, ErrInvalidMoney
	}
	return a * b, nil
}

// roundRatioTo100 uses arbitrary precision intermediates; only the final money
// must fit int64. This permits valid near-int64 ratios without intermediate wrap.
func roundRatioTo100(numerator *big.Int, denominator int64) (int64, error) {
	if numerator.Sign() < 0 || denominator <= 0 {
		return 0, ErrInvalidMoney
	}
	d := new(big.Int).Mul(big.NewInt(denominator), big.NewInt(100))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(numerator, d, rem)
	if new(big.Int).Lsh(rem, 1).Cmp(d) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	q.Mul(q, big.NewInt(100))
	if !q.IsInt64() {
		return 0, ErrInvalidMoney
	}
	return q.Int64(), nil
}

func RoundMoneyTo100(amount int64) (int64, error) { return roundRatioTo100(big.NewInt(amount), 1) }

func MarkedUpPrice(amount int64, percent float64) (int64, error) {
	bps, err := ParsePercent(percent)
	if err != nil || amount < 0 {
		return 0, ErrInvalidMoney
	}
	return markedUpPriceBPS(amount, bps)
}

func markedUpPriceBPS(amount, bps int64) (int64, error) {
	if amount < 0 || bps < 0 || bps > 100000 {
		return 0, ErrInvalidMoney
	}
	n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(percentDenominator+bps))
	price, err := roundRatioTo100(n, percentDenominator)
	if err != nil {
		return 0, err
	}
	if price < amount {
		price = amount
	}
	return price, nil
}

func TruncatedPercent(amount int64, percent float64) (int64, error) {
	bps, err := ParsePercent(percent)
	if err != nil || amount < 0 {
		return 0, ErrInvalidMoney
	}
	n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(bps))
	n.Quo(n, big.NewInt(percentDenominator))
	if !n.IsInt64() {
		return 0, ErrInvalidMoney
	}
	return n.Int64(), nil
}

// DiscountedPrice preserves percent-off-current-final policy, caps the exact
// rational discount at seller margin, rounds the remaining price once, and
// clamps it to [base, final]. Fixed inputs must be integer toman.
func DiscountedPrice(final, base int64, mode string, value float64) (int64, error) {
	if base < 0 || final < base {
		return 0, ErrInvalidMoney
	}
	var discount *big.Int
	denominator := int64(1)
	switch mode {
	case "percent":
		bps, err := ParsePercent(value)
		if err != nil || bps > percentDenominator {
			return 0, ErrInvalidMoney
		}
		discount = new(big.Int).Mul(big.NewInt(final), big.NewInt(bps))
		denominator = percentDenominator
	case "fixed":
		money, err := MoneyFromNumber(value)
		if err != nil {
			return 0, err
		}
		discount = big.NewInt(money)
	default:
		return 0, ErrInvalidMoney
	}
	// A zero discount is no new price event; preserve an exact existing
	// amount (for example a nonrounded sale) rather than rounding it again.
	if discount.Sign() == 0 {
		return final, nil
	}
	cap := new(big.Int).Mul(big.NewInt(final-base), big.NewInt(denominator))
	if discount.Cmp(cap) > 0 {
		discount = cap
	}
	n := new(big.Int).Mul(big.NewInt(final), big.NewInt(denominator))
	n.Sub(n, discount)
	price, err := roundRatioTo100(n, denominator)
	if err != nil {
		return 0, err
	}
	if price < base {
		price = base
	}
	if price > final {
		price = final
	}
	return price, nil
}

// AllocateMoney preserves the established last-positive-index remainder rule.
// Callers supply a stable order (return item ID order for return shipping).
// Zero weights receive zero; all-zero weights place total at the final index.
func AllocateMoney(total int64, weights []int64) ([]int64, error) {
	if total < 0 || (total > 0 && len(weights) == 0) {
		return nil, ErrInvalidMoney
	}
	out := make([]int64, len(weights))
	sum := new(big.Int)
	last := -1
	for i, w := range weights {
		if w < 0 {
			return nil, ErrInvalidMoney
		}
		if w > 0 {
			sum.Add(sum, big.NewInt(w))
			last = i
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	if last < 0 {
		out[len(out)-1] = total
		return out, nil
	}
	var allocated int64
	for i, w := range weights {
		if w == 0 {
			continue
		}
		if i == last {
			out[i] = total - allocated
			break
		}
		n := new(big.Int).Mul(big.NewInt(total), big.NewInt(w))
		n.Quo(n, sum)
		out[i] = n.Int64()
		allocated += out[i]
	}
	return out, nil
}

// PartialRefund is the difference between cumulative floor allocations from an
// immutable line total. This makes any grouping of partial quantities conserve
// the original amount; alreadyRefundedQty must be locked and persisted by caller.
func PartialRefund(original, quantity, alreadyRefundedQty, refundQty int64) (int64, error) {
	if original < 0 || quantity <= 0 || alreadyRefundedQty < 0 || refundQty <= 0 || alreadyRefundedQty > quantity || refundQty > quantity-alreadyRefundedQty {
		return 0, ErrInvalidMoney
	}
	cumulative := func(q int64) *big.Int {
		n := new(big.Int).Mul(big.NewInt(original), big.NewInt(q))
		return n.Quo(n, big.NewInt(quantity))
	}
	return new(big.Int).Sub(cumulative(alreadyRefundedQty+refundQty), cumulative(alreadyRefundedQty)).Int64(), nil
}
