package p9p

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestValid(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(-1, ValidPath([]string{"/", "a"}))
	assert.Equal(-1, ValidPath([]string{"a", "/n"}))
	assert.Equal(-1, ValidPath([]string{"z\\", "a"}))
	assert.Equal(-1, ValidPath([]string{"abbcc", "x\\a"}))
	assert.Equal(-1, ValidPath([]string{"abbcc", ".."}))
	assert.Equal(-1, ValidPath([]string{".", ".."}))
	assert.Equal(0, ValidPath([]string{"x", "y", "z.csv"}))
	assert.Equal(1, ValidPath([]string{"..", "x", "y.dat"}))
	assert.Equal(2, ValidPath([]string{"..", "..", "x.."}))
}

func TestNormalize(t *testing.T) {
	assert := assert.New(t)

	path, n := NormalizePath([]string{"a", "b", "/"})
	assert.Equal(-1, n)
	path, n = NormalizePath([]string{"x", "z\\"})
	assert.Equal(-1, n)
	path, n = NormalizePath([]string{"x", "..", "y", ".", "z"})
	assert.Equal(0, n)
	assert.Equal(path, []string{"y", "z"})
	path, n = NormalizePath([]string{"x", "..", "..", "y", "", "z.npy"})
	assert.Equal(1, n)
	assert.Equal(path, []string{"..", "y", "z.npy"})
}
