package claudecode

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillEnvVars_Empty(t *testing.T) {
	result := SkillEnvVars(nil)
	assert.Nil(t, result)

	result = SkillEnvVars([]Skill{})
	assert.Nil(t, result)
}

func TestSkillEnvVars_InlineSkill(t *testing.T) {
	skills := []Skill{
		{Name: "create-changelog", Inline: "# Create Changelog\n\nDo the thing."},
	}
	result := SkillEnvVars(skills)
	require.NotNil(t, result)

	encoded, ok := result["CLAUDE_SKILL_INLINE_CREATE_CHANGELOG"]
	require.True(t, ok, "expected CLAUDE_SKILL_INLINE_CREATE_CHANGELOG in result")

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	assert.Equal(t, "# Create Changelog\n\nDo the thing.", string(decoded))
}

func TestSkillEnvVars_PathSkill(t *testing.T) {
	skills := []Skill{
		{Name: "my-skill", Path: "/opt/osmia/skills/my-skill.md"},
	}
	result := SkillEnvVars(skills)
	require.NotNil(t, result)

	path, ok := result["CLAUDE_SKILL_PATH_MY_SKILL"]
	require.True(t, ok, "expected CLAUDE_SKILL_PATH_MY_SKILL in result")
	assert.Equal(t, "/opt/osmia/skills/my-skill.md", path)
}

func TestSkillEnvVars_MultipleMixed(t *testing.T) {
	skills := []Skill{
		{Name: "changelog", Inline: "# Changelog\n\nContent."},
		{Name: "review", Path: "/opt/osmia/skills/review.md"},
	}
	result := SkillEnvVars(skills)
	require.NotNil(t, result)

	assert.Contains(t, result, "CLAUDE_SKILL_INLINE_CHANGELOG")
	assert.Contains(t, result, "CLAUDE_SKILL_PATH_REVIEW")
	assert.Len(t, result, 2)
}

func TestSkillEnvVars_SkipEmpty(t *testing.T) {
	// A skill with neither Inline nor Path set produces no env var.
	skills := []Skill{
		{Name: "empty"},
	}
	result := SkillEnvVars(skills)
	assert.Empty(t, result)
}

func TestSkillEnvVars_HyphensAndUnderscores(t *testing.T) {
	tests := []struct {
		name        string
		skill       Skill
		wantKeyPart string
	}{
		{
			name:        "hyphenated name",
			skill:       Skill{Name: "create-changelog", Inline: "x"},
			wantKeyPart: "CLAUDE_SKILL_INLINE_CREATE_CHANGELOG",
		},
		{
			name:        "single word",
			skill:       Skill{Name: "review", Inline: "x"},
			wantKeyPart: "CLAUDE_SKILL_INLINE_REVIEW",
		},
		{
			name:        "multi-hyphen name",
			skill:       Skill{Name: "auto-fix-tests", Inline: "x"},
			wantKeyPart: "CLAUDE_SKILL_INLINE_AUTO_FIX_TESTS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SkillEnvVars([]Skill{tt.skill})
			assert.Contains(t, result, tt.wantKeyPart)
		})
	}
}

func TestSkillEnvVars_InlineContentRoundTrip(t *testing.T) {
	// Verify that multi-line content with special characters survives encoding.
	content := "# Skill\n\nDo:\n- step 1\n- step 2\n\n```bash\necho 'hello'\n```\n"
	skills := []Skill{{Name: "complex", Inline: content}}

	result := SkillEnvVars(skills)
	require.Contains(t, result, "CLAUDE_SKILL_INLINE_COMPLEX")

	decoded, err := base64.StdEncoding.DecodeString(result["CLAUDE_SKILL_INLINE_COMPLEX"])
	require.NoError(t, err)
	assert.Equal(t, content, string(decoded))
}

func TestSkillEnvVars_ConfigMapSkill(t *testing.T) {
	skills := []Skill{
		{Name: "deploy-guide", ConfigMap: "deploy-cm"},
	}
	result := SkillEnvVars(skills)
	require.NotNil(t, result)

	path, ok := result["CLAUDE_SKILL_PATH_DEPLOY_GUIDE"]
	require.True(t, ok, "expected CLAUDE_SKILL_PATH_DEPLOY_GUIDE in result")
	assert.Equal(t, "/skills/deploy-guide.md", path)
}

func TestSkillVolumes_ConfigMap(t *testing.T) {
	skills := []Skill{
		{Name: "changelog", ConfigMap: "changelog-cm"},
		{Name: "review", ConfigMap: "review-cm", Key: "custom-key.md"},
	}

	vols := SkillVolumes(skills)
	require.Len(t, vols, 2)

	assert.Equal(t, "skill-changelog", vols[0].Name)
	assert.Equal(t, "/skills/changelog.md", vols[0].MountPath)
	assert.Equal(t, "changelog.md", vols[0].SubPath)
	assert.True(t, vols[0].ReadOnly)
	assert.Equal(t, "changelog-cm", vols[0].ConfigMapName)
	assert.Equal(t, "changelog.md", vols[0].ConfigMapKey)

	assert.Equal(t, "skill-review", vols[1].Name)
	assert.Equal(t, "custom-key.md", vols[1].SubPath)
	assert.Equal(t, "custom-key.md", vols[1].ConfigMapKey)
}

func TestSkillVolumes_NoConfigMap(t *testing.T) {
	skills := []Skill{
		{Name: "inline-skill", Inline: "content"},
		{Name: "path-skill", Path: "/opt/skills/s.md"},
	}

	vols := SkillVolumes(skills)
	assert.Nil(t, vols)
}

func TestSkillVolumes_DefaultKey(t *testing.T) {
	skills := []Skill{
		{Name: "my-skill", ConfigMap: "skills-cm"},
	}

	vols := SkillVolumes(skills)
	require.Len(t, vols, 1)
	assert.Equal(t, "my-skill.md", vols[0].ConfigMapKey)
	assert.Equal(t, "my-skill.md", vols[0].SubPath)
}

func TestToSafeEnvName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"create-changelog", "CREATE_CHANGELOG"},
		{"review", "REVIEW"},
		{"my_skill", "MY_SKILL"},
		{"auto-fix-tests", "AUTO_FIX_TESTS"},
		{"skill123", "SKILL123"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, toSafeEnvName(tt.input))
		})
	}
}

// TestSkillEnvVars_MultiFileConfigMap covers the directory-style skill: the
// env var names the mount directory rather than a single file, so
// setup-claude.sh knows to copy every Markdown file out of it.
func TestSkillEnvVars_MultiFileConfigMap(t *testing.T) {
	env := SkillEnvVars([]Skill{
		{Name: "incident-classifier", ConfigMap: "classifier-cm", MultiFile: true},
	})

	assert.Equal(t, "/skills/incident-classifier", env["CLAUDE_SKILL_DIR_INCIDENT_CLASSIFIER"])
	assert.NotContains(t, env, "CLAUDE_SKILL_PATH_INCIDENT_CLASSIFIER",
		"a multi-file skill must not also emit the single-file path var")
}

// TestSkillEnvVars_MultiFileIgnoredWithoutConfigMap pins that MultiFile is
// meaningful only alongside a ConfigMap. Inline and path skills are single
// files by construction, so setting the flag on them changes nothing.
func TestSkillEnvVars_MultiFileIgnoredWithoutConfigMap(t *testing.T) {
	env := SkillEnvVars([]Skill{
		{Name: "inline-skill", Inline: "# Inline", MultiFile: true},
		{Name: "path-skill", Path: "/opt/osmia/skills/path-skill.md", MultiFile: true},
	})

	assert.Contains(t, env, "CLAUDE_SKILL_INLINE_INLINE_SKILL")
	assert.Equal(t, "/opt/osmia/skills/path-skill.md", env["CLAUDE_SKILL_PATH_PATH_SKILL"])
	assert.NotContains(t, env, "CLAUDE_SKILL_DIR_INLINE_SKILL")
	assert.NotContains(t, env, "CLAUDE_SKILL_DIR_PATH_SKILL")
}

// TestSkillVolumes_MultiFile covers the volume shape a directory-style skill
// needs: the whole ConfigMap mounted at a directory. Leaving SubPath and
// ConfigMapKey empty is what makes the job builder project every key as its
// own file, so both are asserted explicitly rather than left implied.
func TestSkillVolumes_MultiFile(t *testing.T) {
	mounts := SkillVolumes([]Skill{
		{Name: "incident-classifier", ConfigMap: "classifier-cm", MultiFile: true},
	})

	require.Len(t, mounts, 1)
	assert.Equal(t, "skill-incident-classifier", mounts[0].Name)
	assert.Equal(t, "/skills/incident-classifier", mounts[0].MountPath)
	assert.Equal(t, "classifier-cm", mounts[0].ConfigMapName)
	assert.True(t, mounts[0].ReadOnly)
	assert.Empty(t, mounts[0].SubPath, "a directory mount must not set SubPath")
	assert.Empty(t, mounts[0].ConfigMapKey, "a directory mount must not project a single key")
}

// TestSkillVolumes_MultiFileIgnoresKey pins that Key is dead for a
// directory-style skill, so an operator who leaves a stale key in config does
// not silently get a single-file mount.
func TestSkillVolumes_MultiFileIgnoresKey(t *testing.T) {
	mounts := SkillVolumes([]Skill{
		{Name: "multi", ConfigMap: "cm", Key: "leftover.md", MultiFile: true},
	})

	require.Len(t, mounts, 1)
	assert.Equal(t, "/skills/multi", mounts[0].MountPath)
	assert.Empty(t, mounts[0].ConfigMapKey)
}

// TestSkillVolumes_SingleAndMultiFileMix verifies the two shapes coexist,
// since a deployment can reasonably have both.
func TestSkillVolumes_SingleAndMultiFileMix(t *testing.T) {
	mounts := SkillVolumes([]Skill{
		{Name: "single", ConfigMap: "single-cm"},
		{Name: "multi", ConfigMap: "multi-cm", MultiFile: true},
	})

	require.Len(t, mounts, 2)
	assert.Equal(t, "/skills/single.md", mounts[0].MountPath)
	assert.Equal(t, "single.md", mounts[0].SubPath)
	assert.Equal(t, "/skills/multi", mounts[1].MountPath)
	assert.Empty(t, mounts[1].SubPath)
}
