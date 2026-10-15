package prompt

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFiles creates the files below root, with their parent directories.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
}

func runProjectRules(deps ProjectRulesDeps) result.Result[Option[Section]] {
	return effect.Provide[Option[Section]](deps)(ProjectRules())(context.Background())()
}

// projectRulesIn runs [ProjectRules] over the file system in root.
func projectRulesIn(root string) result.Result[Option[Section]] {
	return runProjectRules(MakeDefaultProjectRulesDeps(tools.MakeWorkspaceDeps(root)))
}

func TestProjectRulesNone(t *testing.T) {
	assert.Equal(t, result.Of(option.None[Section]()), projectRulesIn(t.TempDir()))
}

func TestProjectRulesEmptyFilesGiveNone(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"AGENTS.md":         "\r\n",
		".bob/rules/a.md":   "",
		".bob/rules/b/c.md": "nested files are not rules",
	})

	assert.Equal(t, result.Of(option.None[Section]()), projectRulesIn(root))
}

func TestProjectRulesAgentsMd(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"AGENTS.md": "# AGENTS.md\r\n\r\nUse fp-go.\r\n\r\n"})

	assert.Equal(t,
		result.Of(option.Some(MakeSection(ProjectRulesTag,
			"<agents_md>\n<rule>\n# AGENTS.md\n\nUse fp-go.\n</rule>\n</agents_md>"))),
		projectRulesIn(root),
	)
}

func TestProjectRulesRulesDir(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".bob/rules/b.md":     "rule b\n",
		".bob/rules/a.md":     "rule a",
		".bob/rules/empty.md": "",
		".bob/rules/sub/c.md": "not a rule",
	})

	assert.Equal(t,
		result.Of(option.Some(MakeSection(ProjectRulesTag,
			"<rule>\nrule a\n</rule>\n\n<rule>\nrule b\n</rule>"))),
		projectRulesIn(root),
	)
}

func TestProjectRulesRender(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"AGENTS.md":       "agents",
		".bob/rules/a.md": "rule a",
	})

	assert.Equal(t,
		result.Of(option.Some("<project_rules>\n"+
			"<agents_md>\n<rule>\nagents\n</rule>\n</agents_md>\n\n"+
			"<rule>\nrule a\n</rule>\n"+
			"</project_rules>")),
		result.Map(option.Map(RenderSection()))(projectRulesIn(root)),
	)
}

func TestProjectRulesReadErrorFails(t *testing.T) {
	errDenied := errors.New("permission denied")
	deps := MakeProjectRulesDeps(
		tools.MakeWorkspaceDeps(testRoot),
		tools.MakeFileDeps(func(string) ioresult.IOResult[[]byte] { return ioresult.Left[[]byte](errDenied) }),
		tools.MakeWalkDeps(func(bool) ioresult.Kleisli[string, []tools.FileEntry] {
			return func(string) ioresult.IOResult[[]tools.FileEntry] {
				return ioresult.Left[[]tools.FileEntry](fs.ErrNotExist)
			}
		}),
	)

	assert.Equal(t, result.Left[Option[Section]](errDenied), runProjectRules(deps))
}

func TestProjectRulesListErrorFails(t *testing.T) {
	errDenied := errors.New("permission denied")
	deps := MakeProjectRulesDeps(
		tools.MakeWorkspaceDeps(testRoot),
		tools.MakeFileDeps(func(string) ioresult.IOResult[[]byte] { return ioresult.Left[[]byte](fs.ErrNotExist) }),
		tools.MakeWalkDeps(func(bool) ioresult.Kleisli[string, []tools.FileEntry] {
			return func(string) ioresult.IOResult[[]tools.FileEntry] {
				return ioresult.Left[[]tools.FileEntry](errDenied)
			}
		}),
	)

	assert.Equal(t, result.Left[Option[Section]](errDenied), runProjectRules(deps))
}

func TestProjectRulesReadsWorkspacePaths(t *testing.T) {
	// The reads run concurrently: effect.Ap, under Concat and TraverseArray,
	// evaluates its argument on a second goroutine.
	var (
		mu   sync.Mutex
		read []string
	)
	deps := MakeProjectRulesDeps(
		tools.MakeWorkspaceDeps(testRoot),
		tools.MakeFileDeps(func(path string) ioresult.IOResult[[]byte] {
			return func() result.Result[[]byte] {
				mu.Lock()
				defer mu.Unlock()
				read = append(read, path)
				return result.Of([]byte("text of " + filepath.Base(path)))
			}
		}),
		tools.MakeWalkDeps(func(recursive bool) ioresult.Kleisli[string, []tools.FileEntry] {
			assert.False(t, recursive)
			return func(dir string) ioresult.IOResult[[]tools.FileEntry] {
				assert.Equal(t, filepath.Join(testRoot, ".bob", "rules"), dir)
				return ioresult.Of([]tools.FileEntry{{Path: "x.md"}, {Path: "d", IsDir: true}})
			}
		}),
	)

	assert.Equal(t,
		result.Of(option.Some(MakeSection(ProjectRulesTag,
			"<agents_md>\n<rule>\ntext of AGENTS.md\n</rule>\n</agents_md>\n\n<rule>\ntext of x.md\n</rule>"))),
		runProjectRules(deps),
	)
	assert.ElementsMatch(t,
		[]string{filepath.Join(testRoot, "AGENTS.md"), filepath.Join(testRoot, ".bob", "rules", "x.md")},
		read,
	)
}
