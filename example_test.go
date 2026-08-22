package goq_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/oleexo/goq"
)

// Person is the running example type used across the docs site and here.
type Person struct {
	Name string
	Dept string
	Age  int
}

func ExampleFrom() {
	names := goq.From([]string{"ann", "bob", "cai"}).
		Where(func(s string) bool { return s != "bob" }).
		Select(func(s string) string { return s + "!" }).
		ToSlice()
	fmt.Println(names)
	// Output: [ann! cai!]
}

func ExampleQuery_First() {
	// Absence is a bool, not an error. C#'s FirstOrDefault is this call with
	// the bool discarded.
	v, ok := goq.From([]int{}).First()
	fmt.Println(v, ok)
	// Output: 0 false
}

func ExampleQuery_Single() {
	_, err := goq.From([]int{1, 2}).Single()
	fmt.Println(errors.Is(err, goq.ErrMultiple))
	// Output: true
}

func ExampleQuery_GroupBy() {
	type emp struct{ Name, Dept string }
	staff := []emp{{"Ann", "eng"}, {"Bob", "eng"}, {"Cai", "ops"}}
	out := goq.From(staff).
		GroupBy(func(e emp) string { return e.Dept }).
		OrderBy(func(g goq.Group[string, emp]) string { return g.Key }).
		Select(func(g goq.Group[string, emp]) string {
			return fmt.Sprintf("%s=%d", g.Key, len(g.Items))
		}).
		ToSlice()
	fmt.Println(out)
	// Output: [eng=2 ops=1]
}

func ExampleQuery_SelectErr() {
	_, err := goq.From([]string{"1", "nope"}).
		SelectErr(func(s string) (int, error) {
			if s == "nope" {
				return 0, errors.New("bad input")
			}
			return len(s), nil
		}).
		ToSlice(context.Background())
	fmt.Println(err)
	// Output: bad input
}

func ExampleQuery_AsParallel() {
	// Unordered by default; AsOrdered restores source order.
	out, err := goq.Range(1, 5).
		AsParallel(goq.Workers(4)).
		Select(func(i int) int { return i * i }).
		AsOrdered().
		ToSlice(context.Background())
	fmt.Println(out, err)
	// Output: [1 4 9 16 25] <nil>
}

func ExampleSum() {
	// Sum over numeric elements is a function; Sum with a selector is a method.
	fmt.Println(goq.Sum(goq.Range(1, 4)))
	// Output: 10
}

// ExampleOrderedQuery_ThenByDesc mirrors the "Ordering with a tie-breaker"
// sample on the migrating-from-LINQ page: OrderBy is only reachable through
// ThenBy/ThenByDesc after an initial OrderBy or OrderByDesc call.
func ExampleOrderedQuery_ThenByDesc() {
	people := []Person{
		{Name: "Ann", Dept: "eng", Age: 30},
		{Name: "Bob", Dept: "eng", Age: 40},
		{Name: "Cai", Dept: "ops", Age: 25},
	}
	sorted := goq.From(people).
		OrderBy(func(p Person) string { return p.Dept }).
		ThenByDesc(func(p Person) int { return p.Age }).
		Select(func(p Person) string { return p.Name }).
		ToSlice()
	fmt.Println(sorted)
	// Output: [Bob Ann Cai]
}

// ExampleGroupQuery_OrderByDesc mirrors the "Grouping with aggregation"
// sample on the migrating-from-LINQ page: sort the groups themselves by a
// derived value, here the group size.
func ExampleGroupQuery_OrderByDesc() {
	people := []Person{
		{Name: "Ann", Dept: "eng"},
		{Name: "Bob", Dept: "eng"},
		{Name: "Cai", Dept: "ops"},
	}
	byDept := goq.From(people).
		GroupBy(func(p Person) string { return p.Dept }).
		OrderByDesc(func(g goq.Group[string, Person]) int { return len(g.Items) }).
		Select(func(g goq.Group[string, Person]) string {
			return fmt.Sprintf("%s:%d", g.Key, len(g.Items))
		}).
		ToSlice()
	fmt.Println(byDept)
	// Output: [eng:2 ops:1]
}

// ExampleQuery_Join mirrors the "Join" sample on the migrating-from-LINQ
// page: correlate two pipelines on equal keys.
func ExampleQuery_Join() {
	type order struct {
		Customer string
		Total    int
	}
	orders := []order{{"Ann", 10}, {"Bob", 20}}
	people := []Person{{Name: "Ann"}, {Name: "Bob"}}

	result := goq.From(orders).Join(
		goq.From(people),
		func(o order) string { return o.Customer },
		func(p Person) string { return p.Name },
		func(o order, p Person) string { return fmt.Sprintf("%s:%d", p.Name, o.Total) },
	).ToSlice()
	fmt.Println(result)
	// Output: [Ann:10 Bob:20]
}

// ExampleQuery_ToMap mirrors the "ToDictionary" sample on the
// migrating-from-LINQ page: ToMap reports a duplicate key as
// ErrDuplicateKey rather than silently overwriting or panicking.
func ExampleQuery_ToMap() {
	type order struct {
		ID       int
		Customer string
	}
	orders := []order{{1, "Ann"}, {2, "Bob"}, {1, "Cai"}} // ID 1 repeats

	_, err := goq.From(orders).ToMap(func(o order) int { return o.ID })
	fmt.Println(errors.Is(err, goq.ErrDuplicateKey))

	// ToMapLast makes the "last write wins" choice explicit instead.
	last := goq.From(orders).ToMapLast(func(o order) int { return o.ID })
	fmt.Println(last[1].Customer)
	// Output:
	// true
	// Cai
}

// ExampleParQuery_SelectErr mirrors the "Parallel Select" sample on the
// migrating-from-LINQ page: a fallible projection runs across the worker
// pool, and AsOrdered makes the result deterministic for this example.
func ExampleParQuery_SelectErr() {
	urls := []string{"a", "bb", "ccc"}
	results, err := goq.From(urls).
		AsParallel().
		SelectErr(func(s string) (int, error) { return len(s), nil }).
		AsOrdered().
		ToSlice(context.Background())
	fmt.Println(results, err)
	// Output: [1 2 3] <nil>
}

// ExampleTryQuery_ForEach mirrors the "ForEach: the side-effecting terminal"
// sample on the operators page: ForEach exists only on TryQuery and
// ParQuery, since Query has no error return to report a failed side effect.
func ExampleTryQuery_ForEach() {
	sum := 0
	err := goq.From([]int{1, 2, 3}).AsTry().ForEach(context.Background(), func(x int) error {
		sum += x
		return nil
	})
	fmt.Println(sum, err)
	// Output: 6 <nil>
}

// ExampleParQuery_AsSequential mirrors the "Operator scope is deliberately
// narrow" sample on the async-and-parallel page: AsSequential is the
// explicit barrier back to a TryQuery before operators that require
// materialising the whole stream, such as OrderBy.
func ExampleParQuery_AsSequential() {
	fetched, err := goq.From([]string{"b", "a", "c"}).
		AsParallel().
		SelectErr(func(s string) (string, error) { return s, nil }).
		AsSequential(). // the barrier: back to a TryQuery, out of the pool
		ToSlice(context.Background())
	if err != nil {
		fmt.Println(err)
		return
	}
	sorted := goq.From(fetched).
		OrderBy(func(s string) string { return s }).
		ToSlice()
	fmt.Println(sorted)
	// Output: [a b c]
}

// recoverPanicValue runs a ParQuery pipeline whose callback panics, and
// returns the original panic argument recovered from the goq.PanicValue that
// crosses back onto the caller's goroutine.
func recoverPanicValue() (value any) {
	defer func() {
		if r := recover(); r != nil {
			pv, ok := r.(goq.PanicValue)
			if !ok {
				panic(r) // not one of ours
			}
			value = pv.Value
		}
	}()
	_, _ = goq.From([]int{1, 2, 3}).
		AsParallel(goq.Workers(1)).
		Select(func(i int) int {
			if i == 2 {
				panic("boom")
			}
			return i
		}).
		ToSlice(context.Background())
	return nil
}

// ExamplePanicValue mirrors the "Panics" sample on the async-and-parallel
// page: a callback panic surfaces as a goq.PanicValue panic on the caller's
// goroutine, and callers who recover must type-assert it to reach the
// original value.
func ExamplePanicValue() {
	fmt.Println(recoverPanicValue())
	// Output: boom
}

// ExampleFromChan mirrors the "Re-enumeration" sample on the getting-started
// page: FromChan is single-shot, so a second terminal call reports
// ErrConsumed instead of silently yielding nothing.
func ExampleFromChan() {
	ch := make(chan int, 3)
	ch <- 1
	ch <- 2
	ch <- 3
	close(ch)

	q := goq.FromChan(ch)
	first, err := q.ToSlice(context.Background())
	fmt.Println(first, err)

	second, err := q.ToSlice(context.Background())
	fmt.Println(second, errors.Is(err, goq.ErrConsumed))
	// Output:
	// [1 2 3] <nil>
	// [] true
}

// ExampleQuery_Memoize mirrors the ".Memoize() opts out" sample on the
// getting-started page: Memoize makes any query, including one over a
// single-shot source, safely re-enumerable.
func ExampleQuery_Memoize() {
	q := goq.From([]int{1, 2, 3}).
		Select(func(i int) int { return i * i }).
		Memoize()
	first := q.ToSlice()
	second := q.ToSlice()
	fmt.Println(first, second)
	// Output: [1 4 9] [1 4 9]
}
