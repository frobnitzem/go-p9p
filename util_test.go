package p9p

import (
	"github.com/stretchr/testify/assert"
	"path"
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

	p, n := NormalizePath([]string{"a", "b", "/"})
	assert.Equal(-1, n)
	p, n = NormalizePath([]string{"x", "z\\"})
	assert.Equal(-1, n)
	p, n = NormalizePath([]string{"x", "..", "y", ".", "z"})
	assert.Equal(0, n)
	assert.Equal(p, []string{"y", "z"})
	p, n = NormalizePath([]string{"x", "..", "..", "y", "", "z.npy"})
	assert.Equal(1, n)
	assert.Equal(p, []string{"..", "y", "z.npy"})

	assert.Equal(path.Join("/x", ".."), "/")
}
