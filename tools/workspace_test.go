package tools

import (
	"path/filepath"
	"testing"

	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
)

func TestMakeWorkspaceDeps_CleansRoot(t *testing.T) {
	root := t.TempDir()

	deps := MakeWorkspaceDeps(filepath.Join(root, "a", ".."))

	assert.Equal(t, root, deps.GetWorkspaceRoot())
}

func TestAsWorkspaceDeps(t *testing.T) {
	deps := MakeWorkspaceDeps(t.TempDir())

	assert.Equal(t, deps, AsWorkspaceDeps(deps))
}

func TestAbsolutePath(t *testing.T) {
	root := t.TempDir()
	absolute := absolutePath()(root)

	// a relative path is joined onto the root
	assert.Equal(t, filepath.Join(root, "a", "b"), absolute("a/./b"))
	// an absolute path is only cleaned, even when it lies elsewhere
	other := filepath.Dir(root)
	assert.Equal(t, other, absolute(filepath.Join(other, "x", "..")))
}

func TestOutsideError(t *testing.T) {
	assert.EqualError(t, outsideError("/ws")("/etc"), "path '/etc' is outside the workspace '/ws'")
}

func TestIsWithin(t *testing.T) {
	root := t.TempDir()
	within := isWithin()(root)

	assert.True(t, within(root))
	assert.True(t, within(filepath.Join(root, "a", "b")))
	assert.False(t, within(filepath.Dir(root)))
	assert.False(t, within(root+"-sibling"))
	// filepath.Rel fails for a relative target against an absolute root
	assert.False(t, within("relative"))
}

func TestCheckWithin(t *testing.T) {
	root := t.TempDir()
	check := checkWithin()(root)
	inside := filepath.Join(root, "a")
	outside := filepath.Dir(root)

	assert.Equal(t, result.Of(inside), check(inside))
	assert.Equal(t, result.Left[string](outsideError(root)(outside)), check(outside))
}

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	resolve := ResolveWorkspacePath()(MakeWorkspaceDeps(root))

	for name, tc := range map[string]struct {
		path string
		want string
	}{
		"relative":           {"a/b.txt", filepath.Join(root, "a", "b.txt")},
		"root":               {".", root},
		"absolute inside":    {filepath.Join(root, "x", "..", "y.go"), filepath.Join(root, "y.go")},
		"dot-dot stays in":   {"a/../b", filepath.Join(root, "b")},
		"trailing separator": {"dir" + string(filepath.Separator), filepath.Join(root, "dir")},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, result.Of(tc.want), resolve(tc.path))
		})
	}
}

func TestResolvePathRejects(t *testing.T) {
	root := t.TempDir()
	resolve := ResolvePath()(root)
	outside := filepath.Join(filepath.Dir(root), "other")
	outsideMessage := func(path string) string {
		return "path '" + path + "' is outside the workspace '" + root + "'"
	}

	for name, tc := range map[string]struct {
		path string
		want string
	}{
		"empty":            {"", "path must not be empty"},
		"parent":           {"..", outsideMessage(filepath.Dir(root))},
		"escaping":         {"a/../../other", outsideMessage(outside)},
		"absolute outside": {outside, outsideMessage(outside)},
		"sibling prefix":   {root + "-sibling", outsideMessage(root + "-sibling")},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(resolve(tc.path))
			assert.EqualError(t, err, tc.want)
		})
	}
}
