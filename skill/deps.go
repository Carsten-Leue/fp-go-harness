package skill

import (
	"fmt"
	goio "io"
	"io/fs"
	"os"

	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
)

// SkillDirsDeps give [ReadSkillDirs] the skill directories and a place to
// report the broken skill files it leaves out.
type SkillDirsDeps interface {
	// GetOpenDir opens a skill directory as a file system: [os.DirFS], or a
	// fstest.MapFS in tests.
	GetOpenDir() func(string) fs.FS
	// GetReportBroken reports a skill file that is left out, with the
	// reason.
	GetReportBroken() io.Kleisli[error, F.Void]
}

type skillDirsDeps struct {
	openDir      func(string) fs.FS
	reportBroken io.Kleisli[error, F.Void]
}

func (d *skillDirsDeps) GetOpenDir() func(string) fs.FS             { return d.openDir }
func (d *skillDirsDeps) GetReportBroken() io.Kleisli[error, F.Void] { return d.reportBroken }

// MakeSkillDirsDeps is wiring: it builds [SkillDirsDeps] that open the
// directories with openDir and report broken files with reportBroken.
func MakeSkillDirsDeps(openDir func(string) fs.FS, reportBroken io.Kleisli[error, F.Void]) SkillDirsDeps {
	return &skillDirsDeps{openDir, reportBroken}
}

// reportTo is a leaf, a raw I/O sink: it writes the reason a skill file is
// left out to w, one line each. A failed write is ignored: the report is
// best effort and must not stop the prompt from being assembled.
func reportTo(w goio.Writer) io.Kleisli[error, F.Void] {
	return func(err error) io.IO[F.Void] {
		return func() F.Void {
			_, _ = fmt.Fprintf(w, "skill left out: %v\n", err)
			return F.VOID
		}
	}
}

// MakeDefaultSkillDirsDeps is wiring: it builds [SkillDirsDeps] over the
// operating system's file system that report broken files to stderr.
func MakeDefaultSkillDirsDeps() SkillDirsDeps {
	return MakeSkillDirsDeps(os.DirFS, reportTo(os.Stderr))
}

// SkillsDeps hold the skills that use_skill loads, see [MakeUseSkillTool].
type SkillsDeps interface {
	// GetSkills returns the skills, sorted by name.
	GetSkills() []Skill
}

type skillsDeps struct {
	skills []Skill
}

func (d *skillsDeps) GetSkills() []Skill { return d.skills }

// MakeSkillsDeps is wiring: it builds [SkillsDeps] over skills, such as the
// result of [ReadSkillDirs].
func MakeSkillsDeps(skills []Skill) SkillsDeps {
	return &skillsDeps{skills}
}
