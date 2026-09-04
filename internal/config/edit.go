package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ErrProjectNotFound means the named project is not in the file being edited.
var ErrProjectNotFound = errors.New("project not found in this config file")

// document is a config file held as a YAML node tree so repohop can change one
// project without rewriting the rest. Editing the tree rather than a struct
// preserves comments, key order, and any keys this version of repohop does not
// know about — which matters once editing projects is a routine thing to do
// from the UI rather than something that only ever happens on first setup.
type document struct {
	path string
	root *yaml.Node // the mapping at the top of the file
}

// openDocument reads a config file for editing. A missing or empty file yields
// a fresh document rather than an error, so the first project can be written
// without special-casing.
func openDocument(path string) (*document, error) {
	doc := &document{path: path}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(data)) == 0) {
		doc.root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.setVersion()
		return doc, nil
	}
	if err != nil {
		return nil, err
	}

	var file yaml.Node
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(file.Content) == 0 || file.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: expected a mapping at the top of the file", path)
	}
	doc.root = file.Content[0]
	doc.setVersion()
	return doc, nil
}

// field returns the value node for a top-level key, or nil.
func (d *document) field(key string) *yaml.Node {
	for i := 0; i+1 < len(d.root.Content); i += 2 {
		if d.root.Content[i].Value == key {
			return d.root.Content[i+1]
		}
	}
	return nil
}

// setVersion writes the schema version when the file does not carry one,
// putting it first so the file still reads the way the README describes.
func (d *document) setVersion() {
	if d.field("version") != nil {
		return
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "version"}
	value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(SchemaVersion)}
	d.root.Content = append([]*yaml.Node{key, value}, d.root.Content...)
}

// projects returns the projects sequence, creating it if the file has none.
func (d *document) projects() *yaml.Node {
	if node := d.field("projects"); node != nil {
		if node.Kind != yaml.SequenceNode {
			// An empty `projects:` decodes as a null scalar; make it a list.
			node.Kind = yaml.SequenceNode
			node.Tag = "!!seq"
			node.Value = ""
		}
		return node
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "projects"}
	value := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	d.root.Content = append(d.root.Content, key, value)
	return value
}

// findProject returns the index of a project in the sequence, or -1.
func (d *document) findProject(name string) int {
	for i, entry := range d.projects().Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(entry.Content); j += 2 {
			if entry.Content[j].Value == "name" && entry.Content[j+1].Value == name {
				return i
			}
		}
	}
	return -1
}

// setProject replaces the project with the same name, or appends a new one.
// The replaced entry's own comments are carried over; comments elsewhere in
// the file are untouched.
func (d *document) setProject(spec ProjectSpec) error {
	var encoded yaml.Node
	if err := encoded.Encode(spec); err != nil {
		return err
	}

	projects := d.projects()
	if i := d.findProject(spec.Name); i >= 0 {
		old := projects.Content[i]
		encoded.HeadComment = old.HeadComment
		encoded.LineComment = old.LineComment
		encoded.FootComment = old.FootComment
		projects.Content[i] = &encoded
		return nil
	}
	projects.Content = append(projects.Content, &encoded)
	return nil
}

// renameProject changes a project's name in place, keeping its position in the
// file and everything else about it.
func (d *document) renameProject(from, to string) error {
	i := d.findProject(from)
	if i < 0 {
		return fmt.Errorf("%s: %q: %w", d.path, from, ErrProjectNotFound)
	}
	if from != to && d.findProject(to) >= 0 {
		return fmt.Errorf("%s: a project named %q already exists", d.path, to)
	}
	entry := d.projects().Content[i]
	for j := 0; j+1 < len(entry.Content); j += 2 {
		if entry.Content[j].Value == "name" {
			entry.Content[j+1].Value = to
			return nil
		}
	}
	return fmt.Errorf("%s: %q: %w", d.path, from, ErrProjectNotFound)
}

// removeProject deletes a project from the file.
func (d *document) removeProject(name string) error {
	i := d.findProject(name)
	if i < 0 {
		return fmt.Errorf("%s: %q: %w", d.path, name, ErrProjectNotFound)
	}
	projects := d.projects()
	projects.Content = append(projects.Content[:i], projects.Content[i+1:]...)
	return nil
}

// save writes the document back out.
func (d *document) save() error {
	if err := os.MkdirAll(filepath.Dir(d.path), 0o755); err != nil {
		return err
	}
	data, err := encodeNode(d.root)
	if err != nil {
		return err
	}
	return writeFileAtomic(d.path, data, 0o644)
}

// encodeNode renders a node tree with repohop's indentation.
func encodeNode(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
