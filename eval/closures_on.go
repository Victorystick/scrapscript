//go:build closures

package eval

// closures selects the evaluator that runs the core compiled to Go
// closures, rather than interpreting it with run.
const closures = true
