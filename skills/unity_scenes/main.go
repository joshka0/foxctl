// Package main implements the unity/scenes skill.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const command = "unity/scenes"

const (
	enabledPrefix = "  - enabled: "
	pathPrefix    = "    path: "
	guidPrefix    = "    guid: "
	scenesKey     = "  m_Scenes:"
)

var supportedOps = []string{
	"list",
	"add",
	"remove",
	"enable",
	"disable",
	"reorder",
	"find",
}

type Input struct {
	Operation   string `json:"operation"`
	ScenePath   string `json:"scene_path"`
	Index       int    `json:"index"`
	Pattern     string `json:"pattern"`
	ProjectPath string `json:"project_path"`
}

type Scene struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
	GUID    string `json:"guid,omitempty"`
}

type indexedScene struct {
	Index int `json:"index"`
	Scene
}

func main() {
	skillmain.Main(command, run)
}

func run(_ context.Context, rc *skillmain.RunContext, in Input) error {
	projectPath := strings.TrimSpace(in.ProjectPath)
	if projectPath == "" {
		projectPath = rc.PathValidator.Workspace()
	}
	if !filepath.IsAbs(projectPath) {
		projectPath = filepath.Join(rc.PathValidator.Workspace(), projectPath)
	}

	in.ScenePath = strings.TrimSpace(in.ScenePath)

	projectPath, err := validateProjectPath(projectPath)
	if err != nil {
		return err
	}

	editorBuildSettingsPath := filepath.Join(projectPath, "ProjectSettings", "EditorBuildSettings.asset")
	operation := oputil.Op(in.Operation)
	if operation == "" {
		return skillerr.Arg(
			"operation is required",
			skillerr.WithHint(fmt.Sprintf("Use one of: %s.", strings.Join(supportedOps, ", "))),
		)
	}
	hint := fmt.Sprintf("Use one of: %s.", strings.Join(supportedOps, ", "))

	result, err := oputil.NewSwitch(operation).
		Case("list", func() (map[string]any, error) {
			scenes, err := readScenesFromFile(editorBuildSettingsPath)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"operation": "list",
				"scenes":    scenesWithIndex(scenes),
			}, nil
		}).
		Case("add", func() (map[string]any, error) {
			if in.ScenePath == "" {
				return nil, skillerr.Arg(
					"scene_path is required for add operation",
					skillerr.WithHint("Provide a scene path such as Assets/Scenes/MainMenu.unity."),
				)
			}
			return mutateSceneList("add", editorBuildSettingsPath, in.ScenePath, func(scenes []Scene) ([]Scene, error) {
				for _, scene := range scenes {
					if scene.Path == in.ScenePath {
						return nil, skillerr.Arg(
							fmt.Sprintf("scene already exists in build settings: %s", in.ScenePath),
							skillerr.WithHint("Use remove before adding a scene again."),
						)
					}
				}
				scenes = append(scenes, Scene{
					Enabled: true,
					Path:    in.ScenePath,
				})
				return scenes, nil
			})
		}).
		Case("remove", func() (map[string]any, error) {
			if in.ScenePath == "" {
				return nil, skillerr.Arg(
					"scene_path is required for remove operation",
					skillerr.WithHint("Provide a scene path such as Assets/Scenes/MainMenu.unity."),
				)
			}
			return mutateSceneList("remove", editorBuildSettingsPath, in.ScenePath, func(scenes []Scene) ([]Scene, error) {
				index := findSceneByPath(scenes, in.ScenePath)
				if index < 0 {
					return nil, skillerr.NotFound(
						fmt.Sprintf("scene not found in build settings: %s", in.ScenePath),
						skillerr.WithHint("Add the scene first with operation add."),
					)
				}
				scenes = append(scenes[:index], scenes[index+1:]...)
				return scenes, nil
			})
		}).
		Case("enable", func() (map[string]any, error) {
			if in.ScenePath == "" {
				return nil, skillerr.Arg(
					"scene_path is required for enable operation",
					skillerr.WithHint("Provide a scene path such as Assets/Scenes/MainMenu.unity."),
				)
			}
			return mutateSceneList("enable", editorBuildSettingsPath, in.ScenePath, func(scenes []Scene) ([]Scene, error) {
				index := findSceneByPath(scenes, in.ScenePath)
				if index < 0 {
					return nil, skillerr.NotFound(
						fmt.Sprintf("scene not found in build settings: %s", in.ScenePath),
						skillerr.WithHint("Add the scene first with operation add."),
					)
				}
				scenes[index].Enabled = true
				return scenes, nil
			})
		}).
		Case("disable", func() (map[string]any, error) {
			if in.ScenePath == "" {
				return nil, skillerr.Arg(
					"scene_path is required for disable operation",
					skillerr.WithHint("Provide a scene path such as Assets/Scenes/MainMenu.unity."),
				)
			}
			return mutateSceneList("disable", editorBuildSettingsPath, in.ScenePath, func(scenes []Scene) ([]Scene, error) {
				index := findSceneByPath(scenes, in.ScenePath)
				if index < 0 {
					return nil, skillerr.NotFound(
						fmt.Sprintf("scene not found in build settings: %s", in.ScenePath),
						skillerr.WithHint("Add the scene first with operation add."),
					)
				}
				scenes[index].Enabled = false
				return scenes, nil
			})
		}).
		Case("reorder", func() (map[string]any, error) {
			if in.ScenePath == "" {
				return nil, skillerr.Arg(
					"scene_path is required for reorder operation",
					skillerr.WithHint("Provide a scene path such as Assets/Scenes/MainMenu.unity."),
				)
			}
			return mutateSceneList("reorder", editorBuildSettingsPath, in.ScenePath, func(scenes []Scene) ([]Scene, error) {
				index := findSceneByPath(scenes, in.ScenePath)
				if index < 0 {
					return nil, skillerr.NotFound(
						fmt.Sprintf("scene not found in build settings: %s", in.ScenePath),
						skillerr.WithHint("Add the scene first with operation add."),
					)
				}
				if in.Index < 0 || in.Index >= len(scenes) {
					return nil, skillerr.Arg(
						fmt.Sprintf("index out of range: %d", in.Index),
						skillerr.WithHint(fmt.Sprintf("index must be between 0 and %d", len(scenes)-1)),
					)
				}
				scene := scenes[index]
				scenes = append(scenes[:index], scenes[index+1:]...)
				scenes = insertScene(scenes, scene, in.Index)
				return scenes, nil
			})
		}).
		Case("find", func() (map[string]any, error) {
			files, err := findUnityScenes(projectPath, in.Pattern)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"operation": "find",
				"files":     files,
			}, nil
		}).
		Run()
	if err != nil {
		if _, ok := err.(*oputil.InvalidOpError); ok {
			return skillerr.Arg(err.Error(), skillerr.WithHint(hint))
		}
		return err
	}

	return skillout.Emit(rc, command, result)
}

func mutateSceneList(operation, editorBuildSettingsPath, targetScenePath string, mutator func([]Scene) ([]Scene, error)) (map[string]any, error) {
	lines, start, end, scenes, err := loadSceneConfig(editorBuildSettingsPath)
	if err != nil {
		return nil, err
	}

	updatedScenes, err := mutator(append([]Scene(nil), scenes...))
	if err != nil {
		return nil, err
	}

	updatedLines, err := rewriteSceneSection(lines, start, end, updatedScenes)
	if err != nil {
		return nil, err
	}

	if err := writeLines(editorBuildSettingsPath, updatedLines); err != nil {
		return nil, err
	}

	return map[string]any{
		"operation":  operation,
		"scene_path": targetScenePath,
		"scenes":     scenesWithIndex(updatedScenes),
	}, nil
}

func loadSceneConfig(path string) ([]string, int, int, []Scene, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, 0, 0, nil, err
	}

	start, end := findSceneSection(lines)
	if start == -1 {
		return lines, -1, -1, []Scene{}, nil
	}

	scenes, err := parseSceneSection(lines, start)
	if err != nil {
		return nil, 0, 0, nil, err
	}
	return lines, start, end, scenes, nil
}

func readScenesFromFile(path string) ([]Scene, error) {
	lines, err := readLines(path)
	if err != nil {
		return nil, err
	}

	scenes := make([]Scene, 0)
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], enabledPrefix) {
			scene, _, err := parseSceneEntry(lines, i)
			if err != nil {
				return nil, err
			}
			scenes = append(scenes, scene)
		}
	}
	return scenes, nil
}

func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, skillerr.NotFound(
				fmt.Sprintf("EditorBuildSettings.asset not found at %s", path),
				skillerr.WithHint("Create or open the project in Unity so UnityBuildSettings exists."),
			)
		}
		return nil, skillerr.WrapIO(fmt.Sprintf("open %s", path), err, skillerr.WithHint("Check that the project path is readable."))
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, skillerr.WrapIO(fmt.Sprintf("scan %s", path), err)
	}
	return lines, nil
}

func writeLines(path string, lines []string) error {
	data := strings.Join(lines, "\n")
	if data != "" {
		data += "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return skillerr.WrapIO(fmt.Sprintf("write %s", path), err, skillerr.WithHint("Check write permission for the Unity project directory."))
	}
	return nil
}

func findSceneSection(lines []string) (int, int) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == strings.TrimSpace(scenesKey) {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1
	}

	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "  -") || strings.HasPrefix(line, "    ") {
			continue
		}
		return start, i
	}

	return start, end
}

func parseSceneSection(lines []string, sectionStart int) ([]Scene, error) {
	scenes := make([]Scene, 0)
	for i := sectionStart + 1; i < len(lines); {
		if i >= len(lines) {
			break
		}
		line := lines[i]
		if strings.TrimSpace(line) == "" || !(strings.HasPrefix(line, "  -") || strings.HasPrefix(line, "    ")) {
			return scenes, nil
		}
		if !strings.HasPrefix(line, enabledPrefix) {
			return nil, skillerr.Parse(
				fmt.Sprintf("invalid scene section entry at line %d", i+1),
				skillerr.WithHint("Expected format: '  - enabled: 0|1' followed by '    path: <scene_path>'."),
			)
		}
		scene, next, err := parseSceneEntry(lines, i)
		if err != nil {
			return nil, err
		}
		scenes = append(scenes, scene)
		if next <= i {
			return nil, skillerr.Parse(
				fmt.Sprintf("invalid scene section parsing near line %d", i+1),
				skillerr.WithHint("Expected format: '  - enabled: 0|1' followed by '    path: <scene_path>'."),
			)
		}
		i = next
	}
	return scenes, nil
}

func parseSceneEntry(lines []string, startLine int) (Scene, int, error) {
	if !strings.HasPrefix(lines[startLine], enabledPrefix) {
		return Scene{}, startLine + 1, skillerr.Parse(
			fmt.Sprintf("invalid scene entry at line %d", startLine+1),
			skillerr.WithHint("Expected format: '  - enabled: 0|1' followed by '    path: <scene_path>'."),
		)
	}

	enabledValue := strings.TrimSpace(strings.TrimPrefix(lines[startLine], enabledPrefix))
	enabled, err := strconv.Atoi(enabledValue)
	if err != nil || (enabled != 0 && enabled != 1) {
		return Scene{}, startLine + 1, skillerr.WrapParse(
			fmt.Sprintf("invalid enabled value at line %d", startLine+1),
			err,
			skillerr.WithHint("Expected 0 or 1."),
		)
	}

	pathLineIndex := startLine + 1
	if pathLineIndex >= len(lines) {
		return Scene{}, pathLineIndex + 1, skillerr.WrapParse(
			fmt.Sprintf("missing path for scene entry at line %d", startLine+1),
			nil,
			skillerr.WithHint("Expected format: '  - enabled: 0|1' followed by '    path: <scene_path>'."),
		)
	}
	if !strings.HasPrefix(lines[pathLineIndex], pathPrefix) {
		return Scene{}, pathLineIndex + 1, skillerr.WrapParse(
			fmt.Sprintf("invalid scene path line at line %d", pathLineIndex+1),
			nil,
			skillerr.WithHint("Expected '    path: <path>'."),
		)
	}
	scene := Scene{
		Enabled: enabled == 1,
		Path:    strings.TrimSpace(strings.TrimPrefix(lines[pathLineIndex], pathPrefix)),
	}

	guidLineIndex := pathLineIndex + 1
	next := guidLineIndex
	if guidLineIndex < len(lines) && strings.HasPrefix(lines[guidLineIndex], guidPrefix) {
		scene.GUID = strings.TrimSpace(strings.TrimPrefix(lines[guidLineIndex], guidPrefix))
		next = guidLineIndex + 1
	}

	return scene, next, nil
}

func rewriteSceneSection(lines []string, start, end int, scenes []Scene) ([]string, error) {
	if start == -1 {
		start = insertSceneSection(lines)
		end = start
	}

	rendered := renderSceneSection(scenes)
	updated := make([]string, 0, len(lines)-maxInt(0, end-start)+len(rendered))
	updated = append(updated, lines[:start]...)
	updated = append(updated, rendered...)
	updated = append(updated, lines[end:]...)
	return updated, nil
}

func insertSceneSection(lines []string) int {
	insertAt := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "m_configObjects:") {
			insertAt = i
			break
		}
	}
	return insertAt
}

func renderSceneSection(scenes []Scene) []string {
	lines := make([]string, 0, len(scenes)*3+1)
	lines = append(lines, scenesKey)
	for _, scene := range scenes {
		enabled := "0"
		if scene.Enabled {
			enabled = "1"
		}
		lines = append(lines, enabledPrefix+enabled)
		lines = append(lines, "    path: "+scene.Path)
		if scene.GUID != "" {
			lines = append(lines, "    guid: "+scene.GUID)
		}
	}
	return lines
}

func findSceneByPath(scenes []Scene, scenePath string) int {
	for i, scene := range scenes {
		if scene.Path == scenePath {
			return i
		}
	}
	return -1
}

func insertScene(scenes []Scene, scene Scene, index int) []Scene {
	if index >= len(scenes) {
		return append(scenes, scene)
	}
	scenes = append(scenes[:index], append([]Scene{scene}, scenes[index:]...)...)
	return scenes
}

func scenesWithIndex(scenes []Scene) []indexedScene {
	out := make([]indexedScene, 0, len(scenes))
	for i, scene := range scenes {
		out = append(out, indexedScene{
			Index: i,
			Scene: scene,
		})
	}
	return out
}

func findUnityScenes(projectPath, pattern string) ([]string, error) {
	assetsPath := filepath.Join(projectPath, "Assets")
	pattern = strings.TrimSpace(pattern)

	var files []string
	err := filepath.WalkDir(assetsPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".unity" {
			return nil
		}
		relative, relErr := filepath.Rel(projectPath, path)
		if relErr != nil {
			return skillerr.WrapIO("compute relative path", relErr)
		}
		relative = filepath.ToSlash(relative)
		if pattern != "" {
			// Match pattern against the relative path (e.g. "Assets/Scenes/Main*.unity")
			matched, matchErr := filepath.Match(pattern, relative)
			if matchErr != nil {
				return skillerr.WrapArg("invalid pattern", matchErr, skillerr.WithHint("Use a valid glob pattern (e.g. Assets/Scenes/*.unity). Note: ** is not supported; use single * per path segment."))
			}
			if !matched {
				// Also try matching against just the filename for convenience
				matched, _ = filepath.Match(pattern, d.Name())
				if !matched {
					return nil
				}
			}
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		if _, ok := err.(*os.PathError); ok {
			return nil, skillerr.WrapIO("walk Assets directory", err)
		}
		return nil, err
	}

	sort.Strings(files)
	return files, nil
}

func validateProjectPath(projectPath string) (string, error) {
	assetsPath := filepath.Join(projectPath, "Assets")
	info, err := os.Stat(assetsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", skillerr.NotFound(
				fmt.Sprintf("Assets directory not found in project path %s", projectPath),
				skillerr.WithHint("Provide a valid Unity project path containing an Assets directory."),
			)
		}
		return "", skillerr.WrapIO(fmt.Sprintf("validate project path %s", projectPath), err)
	}
	if !info.IsDir() {
		return "", skillerr.NotFound(
			fmt.Sprintf("Assets path is not a directory: %s", assetsPath),
			skillerr.WithHint("Set project_path to a Unity project root that includes Assets/."),
		)
	}

	return projectPath, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
