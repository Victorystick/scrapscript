package types

import "strings"

// A single substitution.
type Sub struct {
	// Must be a variable.
	replace TypeRef
	with    TypeRef
}

// A set of substitutions.
type Subst []Sub

func (s Subst) binds(target TypeRef) bool {
	for _, s := range s {
		if s.replace == target {
			return true
		}
	}
	return false
}

func (s *Subst) bind(replace, with TypeRef) {
	*s = append(*s, Sub{replace, with})
}

// For debugging.
func (s Subst) String(reg *Registry) string {
	var b strings.Builder

	for i, s := range s {
		if i != 0 {
			b.WriteString(", ")
		}
		b.WriteString(VarString(s.replace))
		b.WriteString(": ")
		b.WriteString(reg.String(s.with))
	}

	return b.String()
}
