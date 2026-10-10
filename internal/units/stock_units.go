// Package units — RestFlow canonical stock units (parity with Nest stock-units.ts).
//
// Convention:
//   mass   → grams (g)
//   volume → millilitres (ml)
//   count  → pcs
//
// When converting kg→g or L→ml: quantity × 1000, unit cost ÷ 1000
// so line value (qty × unitCost) is unchanged.
package units

import (
	"math"
	"strings"
)

type Dimension string

const (
	DimMass   Dimension = "mass"
	DimVolume Dimension = "volume"
	DimCount  Dimension = "count"
	DimOther  Dimension = "other"
)

// NormalizedStockQty is the result of ToCanonicalStockQty.
type NormalizedStockQty struct {
	Quantity  float64
	Unit      string
	UnitCost  float64
	Converted bool
	Dimension Dimension
}

// NormalizeUnit lowercases and maps aliases to a short form.
func NormalizeUnit(unit string) string {
	u := strings.TrimSpace(strings.ToLower(unit))
	if u == "" {
		return "pcs"
	}
	switch u {
	case "kgs", "kilogram", "kilograms":
		return "kg"
	case "grams", "gram":
		return "g"
	case "litre", "liter", "litres", "liters", "lt":
		return "l"
	case "millilitre", "milliliter", "milliliters":
		return "ml"
	case "pieces", "piece", "pc", "unit", "units":
		return "pcs"
	default:
		return u
	}
}

// CanonicalStorageUnit is the unit used on Item / InventoryProjection.
func CanonicalStorageUnit(unit string) string {
	u := NormalizeUnit(unit)
	switch u {
	case "kg", "g":
		return "g"
	case "l", "ml":
		return "ml"
	case "pcs":
		return "pcs"
	default:
		if u == "" {
			return "pcs"
		}
		return u
	}
}

// ToBaseQuantity converts into grams, ml, or pcs when possible.
func ToBaseQuantity(quantity float64, unit string) (value float64, dim Dimension) {
	u := NormalizeUnit(unit)
	q := quantity
	if math.IsNaN(q) || math.IsInf(q, 0) {
		q = 0
	}
	switch u {
	case "kg":
		return q * 1000, DimMass
	case "g":
		return q, DimMass
	case "l":
		return q * 1000, DimVolume
	case "ml":
		return q, DimVolume
	case "pcs":
		return q, DimCount
	default:
		return q, DimOther
	}
}

// ConvertQuantity converts quantity from fromUnit into toUnit when dimensions match.
// On mismatch or "other", returns quantity unchanged.
func ConvertQuantity(quantity float64, fromUnit, toUnit string) float64 {
	fromVal, fromDim := ToBaseQuantity(quantity, fromUnit)
	oneTo, toDim := ToBaseQuantity(1, toUnit)
	if fromDim == DimOther || toDim == DimOther || fromDim != toDim {
		return quantity
	}
	if oneTo == 0 {
		return quantity
	}
	return fromVal / oneTo
}

// ToCanonicalStockQty normalizes qty + unit cost into storage units (g/ml/pcs).
func ToCanonicalStockQty(quantity float64, unit string, unitCost float64) NormalizedStockQty {
	q := quantity
	if math.IsNaN(q) || math.IsInf(q, 0) {
		q = 0
	}
	c := unitCost
	if math.IsNaN(c) || math.IsInf(c, 0) {
		c = 0
	}
	base, dim := ToBaseQuantity(q, unit)
	u := NormalizeUnit(unit)

	switch dim {
	case DimMass:
		costPerG := c
		if u == "kg" {
			costPerG = c / 1000
		}
		return NormalizedStockQty{
			Quantity: base, Unit: "g", UnitCost: costPerG,
			Converted: u == "kg", Dimension: DimMass,
		}
	case DimVolume:
		costPerMl := c
		if u == "l" {
			costPerMl = c / 1000
		}
		return NormalizedStockQty{
			Quantity: base, Unit: "ml", UnitCost: costPerMl,
			Converted: u == "l", Dimension: DimVolume,
		}
	case DimCount:
		return NormalizedStockQty{
			Quantity: q, Unit: "pcs", UnitCost: c,
			Converted: false, Dimension: DimCount,
		}
	default:
		outUnit := u
		if outUnit == "" {
			outUnit = "pcs"
		}
		return NormalizedStockQty{
			Quantity: q, Unit: outUnit, UnitCost: c,
			Converted: false, Dimension: DimOther,
		}
	}
}

// QuantityInItemUnit converts quantity from fromUnit into itemUnit
// (InventoryProjection.available_stock denomination).
func QuantityInItemUnit(quantity float64, fromUnit, itemUnit string) float64 {
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return 0
	}
	from := NormalizeUnit(fromUnit)
	to := NormalizeUnit(itemUnit)
	if to == "" || from == to {
		return quantity
	}
	return ConvertQuantity(quantity, from, to)
}

// ScaleFactorForUnitChange returns the multiplier for on-hand when Item.unit changes.
// kg→g or L→ml → 1000; reverse → 0.001; else 1.
func ScaleFactorForUnitChange(previousUnit, nextUnit string) float64 {
	prev := NormalizeUnit(previousUnit)
	next := NormalizeUnit(nextUnit)
	if prev == next {
		return 1
	}
	if prev == "kg" && next == "g" {
		return 1000
	}
	if prev == "g" && next == "kg" {
		return 0.001
	}
	if prev == "l" && next == "ml" {
		return 1000
	}
	if prev == "ml" && next == "l" {
		return 0.001
	}
	return 1
}

// DedupeMenuLines mirrors Nest/web: keep first line per finishedGoodId, else componentKey.
type MenuLine struct {
	ComponentKey    string
	FinishedGoodID  string
	QuantityRequired float64
}

func DedupeMenuLines(lines []MenuLine) []MenuLine {
	if len(lines) == 0 {
		return nil
	}
	seenKeys := map[string]struct{}{}
	seenIDs := map[string]struct{}{}
	out := make([]MenuLine, 0, len(lines))
	for i, line := range lines {
		id := strings.TrimSpace(line.FinishedGoodID)
		key := strings.TrimSpace(line.ComponentKey)
		if key == "" {
			key = "idx-" + itoa(i)
		}
		if id != "" {
			if _, ok := seenIDs[id]; ok {
				continue
			}
			seenIDs[id] = struct{}{}
		} else if _, ok := seenKeys[key]; ok {
			continue
		}
		seenKeys[key] = struct{}{}
		out = append(out, line)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
