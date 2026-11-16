package skill

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The skill directories in the tests, lowest precedence first.
const (
	homeDir      = "home/.bob/skills"
	workspaceDir = "ws/.bob/skills"
)

// skillFile is a skill file with a name and a description.
func skillFile(name, description string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(skillText(name, description))}
}

// skillText is the text of a skill file with a name, a description and a key
// the harness doesn't read.
func skillText(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\nlicense: MIT\n---\n# " + name + "\n"
}

// openDirs opens the directories of a fixed table; any other directory is
// empty. The table is only read, so the directories can be read
// concurrently.
func openDirs(dirs Record[string, fstest.MapFS]) func(string) fs.FS {
	return func(dir string) fs.FS {
		return dirs[dir]
	}
}

// reports records the reported errors. The record is guarded, because the
// directories are read concurrently.
type reports struct {
	mu   sync.Mutex
	errs []string
}

func (r *reports) report(err error) io.IO[F.Void] {
	return func() F.Void {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.errs = append(r.errs, err.Error())
		return F.VOID
	}
}

func readSkillDirs(dirs Record[string, fstest.MapFS], reported *reports) []Skill {
	return ReadSkillDirs()(MakeSkillDirsDeps(openDirs(dirs), reported.report))([]string{homeDir, workspaceDir})()
}

func TestParseSkill(t *testing.T) {
	text := "---\r\nname: fp-go\r\ndescription: Functional Go.\r\nallowed-tools: [read_file]\r\nmetadata:\r\n  version: 1\r\n---\r\n\r\n# fp-go\r\n"

	assert.Equal(t, result.Of(Skill{
		Name:        "fp-go",
		Description: "Functional Go.",
		Content:     text,
	}), ParseSkill()([]byte(text)))
}

func TestParseSkillErrors(t *testing.T) {
	cases := Record[string, string]{
		"no front matter": "# fp-go\n",
		"unclosed":        "---\nname: fp-go\n# fp-go\n",
		"empty":           "---\n---\n# fp-go\n",
		"no name":         "---\ndescription: Functional Go.\n---\n",
		"not yaml":        "---\nname: [fp-go\n---\n",
		"not a mapping":   "---\n- fp-go\n---\n",
	}

	for name, text := range cases {
		_, err := result.Unwrap(ParseSkill()([]byte(text)))
		assert.Error(t, err, name)
	}

	_, err := result.Unwrap(ParseSkill()([]byte(cases["no name"])))
	assert.EqualError(t, err, "skill without name")
}

func TestDirsIn(t *testing.T) {
	assert.Equal(t, []string{
		filepath.Join("root", ".claude", "skills"),
		filepath.Join("root", ".bob", "skills"),
	}, DirsIn()("root"))
}

func TestReadSkillDirsOverride(t *testing.T) {
	var reported reports
	skills := readSkillDirs(Record[string, fstest.MapFS]{
		homeDir: {
			"fp-go/SKILL.md":   skillFile("fp-go", "Home fp-go."),
			"go-conv/SKILL.md": skillFile("go-conventions", "Go rules."),
			"notes.md":         {Data: []byte("not a skill")},
			"a/b/SKILL.md":     skillFile("nested", "Too deep."),
		},
		workspaceDir: {
			"my-fp-go/SKILL.md": skillFile("fp-go", "Workspace fp-go."),
			"build/SKILL.md":    skillFile("build", "Builds."),
		},
	}, &reported)

	assert.Equal(t, []Skill{
		{
			Name:        "build",
			Description: "Builds.",
			Location:    filepath.Join(workspaceDir, "build", SkillFile),
			Content:     skillText("build", "Builds."),
		},
		{
			Name:        "fp-go",
			Description: "Workspace fp-go.",
			Location:    filepath.Join(workspaceDir, "my-fp-go", SkillFile),
			Content:     skillText("fp-go", "Workspace fp-go."),
		},
		{
			Name:        "go-conventions",
			Description: "Go rules.",
			Location:    filepath.Join(homeDir, "go-conv", SkillFile),
			Content:     skillText("go-conventions", "Go rules."),
		},
	}, skills, "the workspace wins, sorted by name")
	assert.Empty(t, reported.errs)
}

func TestReadSkillDirsBrokenFile(t *testing.T) {
	var reported reports
	skills := readSkillDirs(Record[string, fstest.MapFS]{
		homeDir: {
			"bad/SKILL.md":  {Data: []byte("---\ndescription: no name\n---\n")},
			"good/SKILL.md": skillFile("good", "Works."),
		},
		workspaceDir: {
			"plain/SKILL.md": {Data: []byte("# no front matter\n")},
		},
	}, &reported)

	assert.Equal(t, []string{"good"}, namesOf()(skills), "broken files are left out")
	assert.Equal(t, []string{
		"skill file " + strconv.Quote(filepath.Join(homeDir, "bad", SkillFile)) + ", Caused By: skill without name",
		"skill file " + strconv.Quote(filepath.Join(workspaceDir, "plain", SkillFile)) + ", Caused By: no front matter: the file must start with a line of ---",
	}, reported.errs, "broken files are reported in order")
}

func TestReadSkillDirsMissingDirectory(t *testing.T) {
	var reported reports
	missing := filepath.Join(t.TempDir(), "missing")

	assert.Empty(t, ReadSkillDirs()(MakeSkillDirsDeps(os.DirFS, reported.report))([]string{missing})())
	assert.Empty(t, reported.errs)
}

func TestReportTo(t *testing.T) {
	var buf strings.Builder

	reportTo(&buf)(errors.New("broken"))()

	assert.Equal(t, "skill left out: broken\n", buf.String())
}

func TestReadSkillDirsFromDisk(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "fp-go")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, SkillFile), []byte(skillText("fp-go", "Functional Go.")), 0o644))

	var reported reports
	skills := ReadSkillDirs()(MakeSkillDirsDeps(os.DirFS, reported.report))([]string{root})()

	require.Len(t, skills, 1)
	assert.Equal(t, filepath.Join(root, "fp-go", SkillFile), skills[0].Location)
}
