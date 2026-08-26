package claudecode

import (
	"encoding/base64"
	"regexp"
	"strings"

	"github.com/unitaryai/osmia/pkg/engine"
)

// Skill describes a custom skill file to be made available to the agent.
// Skills are written to ~/.claude/skills/<name>.md before the agent starts,
// allowing the agent to invoke them via /skill-name in its prompts.
//
// Exactly one of Inline, Path, or ConfigMap must be set.
type Skill struct {
	// Name is the skill identifier, used as the filename stem.
	// Use only lowercase letters, digits, and hyphens (e.g. "create-changelog").
	Name string

	// Inline contains the skill Markdown content directly.
	// The content is base64-encoded and passed to the container via an
	// environment variable; setup-claude.sh decodes and writes it at startup.
	Inline string

	// Path is the path to a skill file on the container image
	// (e.g. "/opt/osmia/skills/create-changelog.md").
	// The container's setup-claude.sh copies it to ~/.claude/skills/ at startup.
	Path string

	// ConfigMap is the name of a Kubernetes ConfigMap containing the skill.
	// The ConfigMap is volume-mounted and setup-claude.sh copies the file
	// to ~/.claude/skills/ at startup.
	ConfigMap string

	// Key is the key within the ConfigMap (defaults to "<name>.md").
	// Ignored when MultiFile is true.
	Key string

	// MultiFile indicates the ConfigMap holds a directory-style skill: a
	// SKILL.md entry plus sibling reference files, one ConfigMap key per file.
	// The whole ConfigMap is mounted as a directory and setup-claude.sh copies
	// every Markdown file into the skill's directory. Only meaningful with
	// ConfigMap set; ignored for Inline and Path skills.
	//
	// A skill whose instructions run to thousands of words is better split
	// into a short SKILL.md that points at reference files, so the agent
	// loads the detail only when it needs it.
	MultiFile bool
}

// nonAlphanumRe matches characters that are not letters or digits.
var nonAlphanumRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// SkillEnvVars converts a slice of Skills into environment variable pairs
// suitable for injection into the agent container. setup-claude.sh reads
// these variables and writes the skill files to ~/.claude/skills/ before
// starting the agent.
//
// Inline skills are base64-encoded and stored as:
//
//	CLAUDE_SKILL_INLINE_<SAFE_NAME>=<base64>
//
// Path-based skills use:
//
//	CLAUDE_SKILL_PATH_<SAFE_NAME>=<path>
//
// Directory-style (multi-file) ConfigMap skills use:
//
//	CLAUDE_SKILL_DIR_<SAFE_NAME>=<mount-directory>
//
// SAFE_NAME is the skill name with all non-alphanumeric characters replaced by
// underscores and converted to uppercase (e.g. "create-changelog" → "CREATE_CHANGELOG").
func SkillEnvVars(skills []Skill) map[string]string {
	if len(skills) == 0 {
		return nil
	}
	env := make(map[string]string, len(skills))
	for _, s := range skills {
		safe := toSafeEnvName(s.Name)
		switch {
		case s.Inline != "":
			env["CLAUDE_SKILL_INLINE_"+safe] = base64.StdEncoding.EncodeToString([]byte(s.Inline))
		case s.ConfigMap != "" && s.MultiFile:
			// Directory-style skill: the whole ConfigMap is mounted as a
			// directory; point setup-claude.sh at that directory so it copies
			// every Markdown file into the skill's directory.
			env["CLAUDE_SKILL_DIR_"+safe] = "/skills/" + s.Name
		case s.ConfigMap != "":
			// Single-file ConfigMap skill; point setup-claude.sh at the mounted file.
			env["CLAUDE_SKILL_PATH_"+safe] = "/skills/" + s.Name + ".md"
		case s.Path != "":
			env["CLAUDE_SKILL_PATH_"+safe] = s.Path
		}
	}
	return env
}

// SkillVolumes returns volume mounts for ConfigMap-backed skills. A single-file
// skill gets a SubPath mount of one ConfigMap key at "/skills/<name>.md". A
// multi-file skill mounts the whole ConfigMap as a directory at "/skills/<name>"
// so every key is projected as a file. Distinct mount paths keep skills from
// different ConfigMaps from colliding.
func SkillVolumes(skills []Skill) []engine.VolumeMount {
	var mounts []engine.VolumeMount
	for _, s := range skills {
		if s.ConfigMap == "" {
			continue
		}
		if s.MultiFile {
			// Whole-ConfigMap directory mount: leaving ConfigMapKey and SubPath
			// empty makes buildVolumes project every key as a file under MountPath.
			mounts = append(mounts, engine.VolumeMount{
				Name:          "skill-" + toSafeVolumeName(s.Name),
				MountPath:     "/skills/" + s.Name,
				ReadOnly:      true,
				ConfigMapName: s.ConfigMap,
			})
			continue
		}
		key := s.Key
		if key == "" {
			key = s.Name + ".md"
		}
		mounts = append(mounts, engine.VolumeMount{
			Name:          "skill-" + toSafeVolumeName(s.Name),
			MountPath:     "/skills/" + s.Name + ".md",
			SubPath:       key,
			ReadOnly:      true,
			ConfigMapName: s.ConfigMap,
			ConfigMapKey:  key,
		})
	}
	return mounts
}

// toSafeVolumeName converts a name to a lowercase string safe for use as a
// Kubernetes volume name (lowercase alphanumeric and hyphens only).
func toSafeVolumeName(name string) string {
	safe := nonAlphanumRe.ReplaceAllString(name, "-")
	return strings.ToLower(safe)
}

// toSafeEnvName converts a skill name to an uppercase, underscore-delimited
// string suitable for use as an environment variable name suffix.
// Example: "create-changelog" → "CREATE_CHANGELOG".
func toSafeEnvName(name string) string {
	safe := nonAlphanumRe.ReplaceAllString(name, "_")
	return strings.ToUpper(safe)
}
