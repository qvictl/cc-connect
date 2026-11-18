package config

import "testing"

func TestEffectiveNarration(t *testing.T) {
	message, progress := NarrationMessage, NarrationProgress
	cfg := &Config{}
	if got := EffectiveNarration(cfg, nil); got != NarrationMessage {
		t.Fatalf("default = %q, want message", got)
	}
	cfg.Display.Narration = &progress
	if got := EffectiveNarration(cfg, nil); got != NarrationProgress {
		t.Fatalf("global = %q, want progress", got)
	}
	proj := &ProjectConfig{Display: &DisplayConfig{Narration: &message}}
	if got := EffectiveNarration(cfg, proj); got != NarrationMessage {
		t.Fatalf("project override = %q, want message", got)
	}
	proj.Display.Narration = nil
	if got := EffectiveNarration(cfg, proj); got != NarrationProgress {
		t.Fatalf("global fallback = %q, want progress", got)
	}
}

func TestValidateNarration(t *testing.T) {
	for _, v := range []string{NarrationMessage, NarrationProgress} {
		cfg := Config{Projects: []ProjectConfig{validProject("demo")}}
		cfg.Projects[0].Display = &DisplayConfig{Narration: &v}
		if err := cfg.validate(); err != nil {
			t.Fatalf("narration %q: validate() = %v, want nil", v, err)
		}
	}
	bad := "card"
	cfg := Config{Projects: []ProjectConfig{validProject("demo")}}
	cfg.Projects[0].Display = &DisplayConfig{Narration: &bad}
	assertErrContains(t, cfg.validate(), `projects[0].display.narration must be "message" or "progress"`)
}

func TestNarrationSurvivesSettingsSave(t *testing.T) {
	path := writeConfigFixture(t, `language = "en"
[display]
narration = "progress"
[[projects]]
name = "demo"
[projects.display]
narration = "message"
`)
	patchConfigPath(t, path)
	yes := true
	if err := SaveGlobalSettings(GlobalSettingsUpdate{CommentaryMessages: &yes}); err != nil {
		t.Fatal(err)
	}
	language := "zh"
	if err := SaveProjectSettings("demo", ProjectSettingsUpdate{Language: &language}); err != nil {
		t.Fatal(err)
	}
	cfg := readConfigFixture(t, path)
	if EffectiveNarration(cfg, nil) != NarrationProgress || len(cfg.Projects) != 1 || EffectiveNarration(cfg, &cfg.Projects[0]) != NarrationMessage {
		t.Fatal("unrelated settings save lost narration settings")
	}
}
