package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveEmptyAndDedup(t *testing.T) {
	run := func(input []string, expected []string) func(t *testing.T) {
		return func(t *testing.T) {
			actual := removeEmptyAndDeDup(input)
			assert.Equal(t, expected, actual)
		}
	}
	t.Run("empty", run([]string{}, []string{}))
	t.Run("one", run([]string{"a"}, []string{"a"}))
	t.Run("two", run([]string{"a", "b"}, []string{"a", "b"}))
	t.Run("two with empty", run([]string{"a", ""}, []string{"a"}))
	t.Run("two with empty and dup", run([]string{"a", "", "a", "b"}, []string{"a", "b"}))
}

func TestMergeNotificationsForPath(t *testing.T) {

}
