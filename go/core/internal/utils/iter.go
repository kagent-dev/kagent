package utils

import "iter"

// Map returns an iterator over the slice, applying the function f to each element.
func Map[E any, F any](s iter.Seq[E], f func(E) F) iter.Seq[F] {
	return func(yield func(F) bool) {
		for v := range s {
			if !yield(f(v)) {
				return
			}
		}
	}
}

func Filter[E any](s iter.Seq[E], f func(E) bool) iter.Seq[E] {
	return func(yield func(E) bool) {
		for v := range s {
			if f(v) && !yield(v) {
				return
			}
		}
	}
}
