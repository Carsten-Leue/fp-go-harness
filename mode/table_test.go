package mode

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The directories of the custom modes in the tests.
const (
	homeDir      = "home/.bob/modes"
	workspaceDir = "ws/.bob/modes"
)

// modeFile is a mode file with an id, a name and an order.
func modeFile(id, name, order string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\nid: " + id + "\nname: " + name + "\norder: " + order + "\ntools: [grep]\n---\nYou are " + name + ".\n")}
}

// openDirs opens the directories of a fixed table; any other directory is
// empty. The table is only read, so the directories can be read
// concurrently.
func openDirs(dirs Record[string, fstest.MapFS]) func(string) fs.FS {
	return func(dir string) fs.FS {
		return dirs[dir]
	}
}

func readModeDirs(t *testing.T, dirs Record[string, fstest.MapFS]) []Mode {
	t.Helper()

	modes, err := result.Unwrap(ReadModeDirs()(openDirs(dirs))([]string{homeDir, workspaceDir})())
	require.NoError(t, err)
	return modes
}

func TestReadModeDirsWithoutCustomModes(t *testing.T) {
	assert.Equal(t, Modes(), readModeDirs(t, nil))
}

func TestReadModeDirsMissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	assert.Equal(t, result.Of(Modes()), ReadModeDirs()(os.DirFS)([]string{missing})())
}

func TestReadModeDirsOverrideAndNewMode(t *testing.T) {
	modes := readModeDirs(t, Record[string, fstest.MapFS]{
		homeDir: {
			"ask.md":    modeFile(AskID, "Home Ask", "3"),
			"plan.md":   modeFile(PlanID, "Home Plan", "2"),
			"review.md": modeFile("review", "Review", "4"),
		},
		workspaceDir: {
			"my-ask.md": modeFile(AskID, "Workspace Ask", "3"),
			"first.md":  modeFile("first", "First", "0"),
		},
	})

	assert.Equal(t, []string{"first", AgentID, PlanID, AskID, "review"}, idsOf()(modes))

	table := MakeModeTableDeps(modes)
	find := func(id string) Mode {
		m, err := result.Unwrap(Lookup()(table)(id))
		require.NoError(t, err)
		return m
	}

	assert.Equal(t, "Workspace Ask", find(AskID).Name, "the workspace wins over the home directory")
	assert.Equal(t, "Home Plan", find(PlanID).Name, "the home directory wins over the built-in modes")
	assert.Equal(t, "Agent", find(AgentID).Name, "a built-in mode without an override stays")
	assert.Equal(t, Mode{
		ID:           "review",
		Name:         "Review",
		Order:        4,
		AllowedTools: []string{"grep"},
		Prompt:       "You are Review.",
	}, find("review"))

	_, err := result.Unwrap(Lookup()(table)("code"))
	assert.EqualError(t, err, `unknown mode "code", use one of first, agent, plan, ask, review`)
}

func TestReadModeDirsDuplicateID(t *testing.T) {
	_, err := result.Unwrap(ReadModeDirs()(openDirs(Record[string, fstest.MapFS]{
		homeDir: {"review.md": modeFile("review", "Review", "4")},
		workspaceDir: {
			"a.md": modeFile("review", "Review A", "4"),
			"b.md": modeFile("review", "Review B", "4"),
		},
	}))([]string{homeDir, workspaceDir})())

	assert.EqualError(t, err, `mode directory "ws/.bob/modes", Caused By: mode "review" is defined twice`)
}

func TestReadModeDirsBrokenFile(t *testing.T) {
	_, err := result.Unwrap(ReadModeDirs()(openDirs(Record[string, fstest.MapFS]{
		homeDir: {"bad.md": {Data: []byte("---\nid: bad\ntools: [rm]\n---\n")}},
	}))([]string{homeDir, workspaceDir})())

	assert.EqualError(t, err, `mode directory "home/.bob/modes", Caused By: mode file "bad.md", Caused By: mode "bad" allows unknown tools rm`)
}

func TestIndexModes(t *testing.T) {
	a := Mode{ID: "a", Name: "A"}
	b := Mode{ID: "b", Name: "B"}

	assert.Equal(t, result.Of(Record[string, Mode]{"a": a, "b": b}), IndexModes()([]Mode{a, b}))

	_, err := result.Unwrap(IndexModes()([]Mode{a, b, a, a}))
	assert.EqualError(t, err, `mode "a" is defined twice`)
}

func TestMergeModes(t *testing.T) {
	low := Record[string, Mode]{
		"x": {ID: "x", Name: "low x", Order: 1},
		"c": {ID: "c", Order: 2},
	}
	high := Record[string, Mode]{
		"x": {ID: "x", Name: "high x", Order: 2},
		"a": {ID: "a", Order: 2},
	}

	assert.Equal(t, []Mode{
		{ID: "a", Order: 2},
		{ID: "c", Order: 2},
		{ID: "x", Name: "high x", Order: 2},
	}, MergeModes()([]Record[string, Mode]{low, high}), "the last table wins, the same order sorts by id")

	assert.Empty(t, MergeModes()(nil))
}
