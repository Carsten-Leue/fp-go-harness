// Package skill discovers the skills installed for the user and the
// workspace, renders them into the available_skills section of the system
// prompt and offers the use_skill tool that loads one.
//
// A skill is a directory with a SKILL.md file: Markdown with YAML front
// matter that holds at least its name and a description. [ParseSkill] parses
// one file, [ReadSkillDirs] every <dir>/<skill>/SKILL.md of skill
// directories such as ~/.bob/skills and <workspace>/.claude/skills (see
// [DirsIn]), merged by name: a later directory wins. A broken file is left
// out and reported, so the prompt is still assembled.
//
// [AvailableSkills] renders the skills as the available_skills section,
// [MakeUseSkillTool] builds use_skill over [SkillsDeps], and
// [MakeSharedDeps] hands both to every mode as [mode.SharedDeps]: package
// mode can't import this one, which uses [mode.SplitFrontMatter].
package skill

//go:generate go tool gen lens
