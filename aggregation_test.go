package goq_test

import (
	"math"
	"testing"

	"github.com/oleexo/goq"
)

type lineItem struct {
	SKU   string
	Price float64
	Qty   int
}

func items() []lineItem {
	return []lineItem{{"a", 1.50, 2}, {"b", 10.00, 1}, {"c", 4.25, 7}}
}

func TestAggregate(t *testing.T) {
	t.Parallel()
	got := goq.From([]int{1, 2, 3}).Aggregate(0, func(acc, v int) int { return acc + v })
	if got != 6 {
		t.Errorf("Aggregate = %d, want 6", got)
	}
	// Seed is returned untouched for an empty source.
	if got := goq.Empty[int]().Aggregate(99, func(a, v int) int { return a + v }); got != 99 {
		t.Errorf("Aggregate on empty = %d, want 99", got)
	}
	// The accumulator type may differ from the element type.
	joined := goq.From([]int{1, 2}).Aggregate("", func(acc string, v int) string {
		return acc + string(rune('0'+v))
	})
	if joined != "12" {
		t.Errorf("Aggregate = %q, want \"12\"", joined)
	}
}

func TestSumMethodAndFunction(t *testing.T) {
	t.Parallel()
	q := goq.From(items())
	if got := q.Sum(func(l lineItem) float64 { return l.Price }); math.Abs(got-15.75) > 1e-9 {
		t.Errorf("Sum(price) = %v, want 15.75", got)
	}
	if got := q.Sum(func(l lineItem) int { return l.Qty }); got != 10 {
		t.Errorf("Sum(qty) = %d, want 10", got)
	}
	if got := goq.Sum(goq.From([]int{1, 2, 3})); got != 6 {
		t.Errorf("Sum = %d, want 6", got)
	}
	if got := goq.Sum(goq.Empty[int]()); got != 0 {
		t.Errorf("Sum on empty = %d, want 0", got)
	}
	if got := goq.Empty[lineItem]().Sum(func(l lineItem) float64 { return l.Price }); got != 0 {
		t.Errorf("Sum method on empty = %v, want 0", got)
	}
}

func TestAverage(t *testing.T) {
	t.Parallel()
	got, ok := goq.From(items()).Average(func(l lineItem) float64 { return l.Price })
	if !ok || math.Abs(got-5.25) > 1e-9 {
		t.Errorf("Average = (%v, %v), want (5.25, true)", got, ok)
	}
	// Empty must report false rather than divide by zero.
	if _, ok := goq.Empty[int]().Average(func(i int) int { return i }); ok {
		t.Error("Average on empty reported ok")
	}
	if _, ok := goq.Average(goq.Empty[float64]()); ok {
		t.Error("package Average on empty reported ok")
	}
}

func TestCount(t *testing.T) {
	t.Parallel()
	if got := goq.From([]int{1, 2, 3}).Count(); got != 3 {
		t.Errorf("Count = %d, want 3", got)
	}
	if got := goq.Empty[int]().Count(); got != 0 {
		t.Errorf("Count on empty = %d, want 0", got)
	}
}

func TestMinMax(t *testing.T) {
	t.Parallel()
	if v, ok := goq.Min(goq.From([]string{"pear", "apple"})); !ok || v != "apple" {
		t.Errorf("Min = (%v, %v), want (apple, true)", v, ok)
	}
	if v, ok := goq.Max(goq.From([]int{3, 9, 1})); !ok || v != 9 {
		t.Errorf("Max = (%v, %v), want (9, true)", v, ok)
	}
	if _, ok := goq.Min(goq.Empty[int]()); ok {
		t.Error("Min on empty reported ok")
	}
	if _, ok := goq.Max(goq.Empty[int]()); ok {
		t.Error("Max on empty reported ok")
	}
	// MinBy/MaxBy return the ELEMENT, not the key.
	if v, ok := goq.From(items()).MaxBy(func(l lineItem) float64 { return l.Price }); !ok || v.SKU != "b" {
		t.Errorf("MaxBy = (%v, %v), want SKU b", v, ok)
	}
	if v, ok := goq.From(items()).MinBy(func(l lineItem) float64 { return l.Price }); !ok || v.SKU != "a" {
		t.Errorf("MinBy = (%v, %v), want SKU a", v, ok)
	}
	// Ties resolve to the first element encountered.
	tie := []lineItem{{"first", 1, 0}, {"second", 1, 0}}
	if v, _ := goq.From(tie).MinBy(func(l lineItem) float64 { return l.Price }); v.SKU != "first" {
		t.Errorf("MinBy tie = %v, want first", v.SKU)
	}
	if _, ok := goq.Empty[lineItem]().MinBy(func(l lineItem) float64 { return l.Price }); ok {
		t.Error("MinBy on empty reported ok")
	}
	if _, ok := goq.Empty[lineItem]().MaxBy(func(l lineItem) float64 { return l.Price }); ok {
		t.Error("MaxBy on empty reported ok")
	}
}
