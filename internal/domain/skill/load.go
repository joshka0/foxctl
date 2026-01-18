package skill

import (
	"fmt"
	"path/filepath"
)

// LoadManifestAndArtifact loads a manifest and resolves its artifact path.
// It validates the WASI policy before returning the manifest.
func LoadManifestAndArtifact(manifestPath string, opts ArtifactOptions) (Manifest, string, error) {
	if manifestPath == "" {
		return Manifest{}, "", fmt.Errorf("manifest path required")
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return Manifest{}, "", err
	}

	if err := ValidateWASIPolicy(manifest); err != nil {
		return Manifest{}, "", err
	}

	artifactPath, err := ResolveArtifactPath(filepath.Dir(manifestPath), manifest, opts)
	if err != nil {
		return Manifest{}, "", err
	}

	return manifest, artifactPath, nil
}

// LoadManifestAndArtifactFromDir loads a manifest and resolves its artifact path
// using a directory containing skill.yaml.
func LoadManifestAndArtifactFromDir(dir string, opts ArtifactOptions) (Manifest, string, error) {
	if dir == "" {
		return Manifest{}, "", fmt.Errorf("skill directory required")
	}
	return LoadManifestAndArtifact(filepath.Join(dir, "skill.yaml"), opts)
}
