package dsl

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func LoadDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read DB-DSL file: %w", err)
	}

	var top map[string]any
	if err := yaml.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parse DB-DSL YAML: %w", err)
	}

	var doc Document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode DB-DSL YAML: %w", err)
	}

	doc.TopLevelKeys = make(map[string]bool, len(top))
	for key := range top {
		doc.TopLevelKeys[key] = true
	}

	return &doc, nil
}

func LoadReviewedSource(path string) (*ReviewedSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read reviewed source file: %w", err)
	}

	var source ReviewedSource
	if err := yaml.Unmarshal(data, &source); err != nil {
		return nil, fmt.Errorf("parse reviewed source YAML: %w", err)
	}

	return &source, nil
}

func LoadBundle(path string) (*Document, *ReviewedSource, string, error) {
	doc, err := LoadDocument(path)
	if err != nil {
		return nil, nil, "", err
	}

	sourcePath := ResolveReviewedSourcePath(path, doc.Source.ReviewedFragmentsFile)
	source, err := LoadReviewedSource(sourcePath)
	if err != nil {
		return nil, nil, sourcePath, err
	}

	return doc, source, sourcePath, nil
}

func LoadV06Bundle(path string) (*Bundle, error) {
	doc, err := LoadDocument(path)
	if err != nil {
		return nil, err
	}
	if doc.DSL.Version != "0.6" {
		return nil, fmt.Errorf("load v0.6 bundle: dsl.version is %s", doc.DSL.Version)
	}

	bundle := &Bundle{
		ModelPath: path,
		Document:  doc,
	}

	var sourceUnits SourceUnitsFile
	if bundle.SourceUnitsPath, err = loadYAMLResource(path, "source.source_units_file", doc.Source.SourceUnitsFile, &sourceUnits); err != nil {
		return nil, err
	}
	bundle.SourceUnits = &sourceUnits

	var reviewDecisions ReviewDecisionsFile
	if bundle.ReviewDecisionsPath, err = loadYAMLResource(path, "source.review_decisions_file", doc.Source.ReviewDecisionsFile, &reviewDecisions); err != nil {
		return nil, err
	}
	bundle.ReviewDecisions = &reviewDecisions

	return bundle, nil
}

func ResolveReviewedSourcePath(dslPath, sourcePath string) string {
	if sourcePath == "" || filepath.IsAbs(sourcePath) {
		return sourcePath
	}
	if _, err := os.Stat(sourcePath); err == nil {
		return sourcePath
	}
	return filepath.Join(filepath.Dir(dslPath), sourcePath)
}

func loadYAMLResource(modelPath, fieldName, resourcePath string, target any) (string, error) {
	if resourcePath == "" {
		return "", fmt.Errorf("%s is required for DB-DSL v0.6", fieldName)
	}
	resolvedPath := ResolveModelResourcePath(modelPath, resourcePath)
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return resolvedPath, fmt.Errorf("read %s: %w", fieldName, err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		return resolvedPath, fmt.Errorf("parse %s YAML: %w", fieldName, err)
	}
	return resolvedPath, nil
}

func ResolveModelResourcePath(modelPath, resourcePath string) string {
	if resourcePath == "" || filepath.IsAbs(resourcePath) {
		return resourcePath
	}
	if _, err := os.Stat(resourcePath); err == nil {
		return resourcePath
	}

	modelDir := filepath.Dir(modelPath)
	adjacent := filepath.Join(modelDir, resourcePath)
	if _, err := os.Stat(adjacent); err == nil {
		return adjacent
	}

	absModelPath, err := filepath.Abs(modelPath)
	if err != nil {
		return adjacent
	}
	dir := filepath.Dir(absModelPath)
	for {
		candidate := filepath.Join(dir, resourcePath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return adjacent
}
